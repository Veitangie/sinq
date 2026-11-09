// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"time"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/luapi"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/sinqerror"
	"veitangie.dev/sinq/internal/timer"
)

type Workspace interface {
	fs.StatFS
	Create(string) (io.WriteCloser, error)
	MkDirall(string) error
	String() string
}

type taskBundle struct {
	scenario.ScenarioBlueprint
	runId     event.RunId
	workspace Workspace
	env       map[string]any
	labels    []string
	run       bool
}

type workerEnv struct {
	cfg             config.Config
	secrets         map[string]any
	eb              *event.Bus
	transport       http.RoundTripper
	clock           timer.Clock
	compiler        *cachedCompiler
	loader          *cachedLoader
	workspace       Workspace
	cachedProcessor *cachedRequestProcessor
	hasher          maphash.Hash
}

type worker struct {
	id                int
	env               workerEnv
	taskCh            <-chan taskBundle
	stageIdx          int
	maxStageIdx       int
	lc                *luapi.LuaContext
	assertionFailures []string
	reportCtx         context.Context
	seq               int
}

type workerContextKey string

const (
	scenarioNameContext workerContextKey = "scenarioName"
	stageNameContext    workerContextKey = "stageName"
	stageIdxContext     workerContextKey = "stageIdx"
	runIdContext        workerContextKey = "runId"
)

func (w *worker) loggingContext(ctx context.Context) []slog.Attr {
	res := make([]slog.Attr, 0, 2)
	if scenarioName, ok := ctx.Value(scenarioNameContext).(string); ok {
		res = append(
			res,
			slog.Attr{
				Key:   string(scenarioNameContext),
				Value: slog.StringValue(scenarioName),
			},
		)
	}
	if stageName, ok := ctx.Value(stageNameContext).(string); ok {
		res = append(
			res,
			slog.Attr{
				Key:   string(stageNameContext),
				Value: slog.StringValue(stageName),
			},
		)
	}
	return res
}

func (w *worker) newEvent(ctx context.Context, kind event.EventType) event.Event {
	runId, _ := ctx.Value(runIdContext).(event.RunId)
	stageIdx, _ := ctx.Value(stageIdxContext).(int)
	return event.Event{
		Kind:     kind,
		RunId:    runId,
		StageIdx: stageIdx,
		WorkerId: w.id,
		Source:   event.Runner,
	}
}

func (w *worker) log(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) event.Event {
	e := w.newEvent(ctx, event.Log)
	e.Level = level
	e.Message = msg
	e.Attrs = append(w.loggingContext(ctx), attrs...)
	return e
}

func (w *worker) stageEnd(ctx context.Context, kind event.EventType, duration time.Duration) event.Event {
	e := w.newEvent(ctx, kind)
	e.Duration = duration
	return e
}

func (w *worker) scenarioEnd(ctx context.Context, scenarioTimer timer.Timer) event.Event {
	e := w.stageEnd(ctx, event.ScenarioEnd, scenarioTimer.Time())
	if w.lc != nil {
		e.Output = w.lc.Printer
	}
	return e
}

func (w *worker) send(es ...event.Event) {
	for i := range es {
		w.seq++
		es[i].WorkerSeq = w.seq
	}
	ctx := w.reportCtx
	if ctx == nil {
		ctx = context.Background()
	}
	w.env.eb.Send(ctx, es...)
}

func (w *worker) run(ctx context.Context) {
	if ctx == nil {
		e := w.newEvent(context.Background(), event.WorkerError)
		e.ErrorString = "Worker.run() called with nil context"
		w.send(e)
		return
	}

Scenarios:
	for {
		select {
		case task, ok := <-w.taskCh:
			if !ok {
				break Scenarios
			}

			ctxTimeout, cancel := context.WithTimeout(ctx, task.Config.Timeout.Duration)
			w.processScenario(context.WithValue(ctxTimeout, scenarioNameContext, task.Config.Name), task)
			cancel()
		case <-ctx.Done():
			break Scenarios
		}
	}
}

func (w *worker) processStage(ctx context.Context, scenarioBp *scenario.ScenarioBlueprint, stageIdx int, client *http.Client, timeout time.Duration) bool {
	ctx = context.WithValue(ctx, stageIdxContext, stageIdx)
	stageTimer := timer.NewTimer(w.env.clock)
	processor := RequestProcessor{
		w:          w,
		ctx:        ctx,
		scenarioBp: scenarioBp,
		stageBp:    scenarioBp.Stages[stageIdx],
		stageIdx:   stageIdx,
		stageTimer: stageTimer,
		client:     client,
		reqTimeout: timeout,
	}
	if err := ctx.Err(); err != nil {
		processor.processError(ctx, err, time.Duration(0))
		return false
	}
	start := w.newEvent(ctx, event.StageStart)
	start.StartedAt = w.env.clock.Now()
	start.Filename = scenarioBp.Stages[stageIdx].Filename
	w.send(start)

	w.lc.SetupStageEnvironment(stageIdx, !scenarioBp.Stages[stageIdx].IsNoRequest())

	defer func() {
		v := recover()
		if v != nil {
			e := processor.stageEnd(ctx, stageTimer.Time())
			e.Status = event.Error
			e.ErrorString = fmt.Sprintf("Caught a panic while executing stage: %v", v)
			w.send(e)
		}
	}()

	if err := processor.runPre(); err != nil {
		processor.processError(ctx, err, stageTimer.Time())
		return false
	}

	if processor.skip {
		e := w.stageEnd(ctx, event.StageEnd, stageTimer.Time())
		e.Status = event.Skipped
		w.send(e)
		return true
	}

	if !scenarioBp.Stages[stageIdx].IsNoRequest() {
		if err := processor.materialize(); err != nil {
			processor.processError(ctx, err, stageTimer.Time())
			return false
		}

		if err := processor.parse(); err != nil {
			processor.processError(ctx, err, stageTimer.Time())
			return false
		}

		if err := processor.run(); err != nil {
			processor.processError(ctx, err, stageTimer.Time())
			return false
		}
	}

	if err := processor.runAssert(); err != nil {
		processor.processError(ctx, err, stageTimer.Time())
		return false
	}

	if len(w.assertionFailures) > 0 && scenarioBp.Config.FailFast {
		e := processor.stageEnd(ctx, stageTimer.Time())
		e.FailedAssertions = w.assertionFailures
		e.Status = event.Failure
		w.send(e)
		w.assertionFailures = make([]string, 0)
		return false
	}

	if err := processor.runPost(); err != nil {
		processor.processError(ctx, err, stageTimer.Time())
		return false
	}

	e := processor.stageEnd(ctx, stageTimer.Time())
	e.Status = event.Success
	if len(w.assertionFailures) > 0 {
		e.FailedAssertions = w.assertionFailures
		e.Status = event.Failure
		w.assertionFailures = make([]string, 0)
	}
	w.send(e)
	return true
}

func (w *worker) processScenario(ctx context.Context, bundle taskBundle) {
	ctx = context.WithValue(ctx, runIdContext, bundle.runId)
	stageNames := make([]event.StageName, 0, len(bundle.Stages))
	for _, stage := range bundle.Stages {
		name := stage.Name
		if name == "" {
			name = stage.Filename
		}
		stageNames = append(stageNames, event.StageName{Name: name, Filename: filepath.Join(stage.Rootname, stage.Filename)})
	}

	scenarioTimer := timer.NewTimer(w.env.clock)
	start := w.newEvent(ctx, event.ScenarioStart)
	start.Tags = bundle.Config.TagsList
	start.Labels = bundle.labels
	start.Name = bundle.Config.Name
	start.TotalRunCount = w.env.cfg.Count
	start.StartedAt = scenarioTimer.StartedAt()
	start.StageNames = stageNames
	if !bundle.run {
		start.Flags = start.Flags | event.SkippedScenarioMask
		start.TotalRunCount = 0
	}
	w.send(w.log(ctx, slog.LevelDebug, "[Runner] Starting scenario"), start)

	if !bundle.run {
		w.send(w.scenarioEnd(ctx, scenarioTimer))
		return
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		end := w.newEvent(ctx, event.StageEnd)
		end.ErrorString = "Failed to create cookie jar"
		end.Status = event.Error
		end.StageIdx = 0
		w.send(
			w.log(ctx, slog.LevelWarn, "[Runner] Failed to create cookiejar for scenario", event.ErrAttr(err)),
			end,
			w.scenarioEnd(ctx, scenarioTimer),
		)
		return
	}

	client := http.Client{
		Transport: w.env.transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > bundle.Config.MaxRedirects {
				return sinqerror.ErrTooManyRedirects
			}
			return nil
		},
	}

	w.env.workspace = bundle.workspace
	defer func() {
		w.env.workspace = nil
		if r := recover(); r != nil {
			if w.lc != nil {
				w.lc.SetContext(ctx)
			}
			end := w.scenarioEnd(ctx, scenarioTimer)
			end.Flags = end.Flags | event.ErrorScenarioMask
			end.ErrorString = fmt.Sprintf("%v", r)
			w.send(
				w.log(ctx, slog.LevelWarn, "[Runner] Panic during scenario run"),
				w.log(ctx, slog.LevelDebug, "[Runner] More detailed info on panic", slog.Any("panic", r)),
				end,
			)
		}
	}()

	w.stageIdx = 0
	w.maxStageIdx = len(bundle.Stages) - 1
	w.assertionFailures = make([]string, 0)
	err = w.setupScenarioEnvironment(ctx, bundle.env)
	if err != nil {
		workerErr := w.newEvent(ctx, event.WorkerError)
		workerErr.Error = err
		end := w.newEvent(ctx, event.StageEnd)
		end.ErrorString = "Failed to set up lua for scenario"
		end.Status = event.Error
		end.StageIdx = 0
		w.send(
			workerErr,
			w.log(ctx, slog.LevelWarn, "[Runner] Failed to set up lua environment for scenario", event.ErrAttr(err)),
			end,
			w.scenarioEnd(ctx, scenarioTimer),
		)
		return
	}
	defer w.teardownScenarioEnvironment()

	shouldContinue := w.stageIdx >= 0 && w.stageIdx < len(bundle.Stages)
	for shouldContinue {
		oldStageIdx := w.stageIdx

		w.stageIdx++

		shouldContinue = w.processStage(
			context.WithValue(ctx, stageNameContext, bundle.Stages[oldStageIdx].Filename),
			&bundle.ScenarioBlueprint,
			oldStageIdx,
			&client,
			bundle.Config.ReqTimeout.Duration,
		)

		shouldContinue = shouldContinue && w.stageIdx < len(bundle.Stages)
	}

	w.send(w.scenarioEnd(ctx, scenarioTimer))
}

func (w *worker) materializeLValue(value lua.LValue) (string, error) {
	switch typed := value.(type) {
	case *lua.LUserData:
		return w.lc.SerializeLValue(typed)
	case *lua.LTable:
		return w.lc.SerializeLValue(typed)
	case lua.LBool:
		return typed.String(), nil
	case lua.LNumber:
		return typed.String(), nil
	case lua.LString:
		return string(typed), nil
	}
	return "", fmt.Errorf("unexpected type %s returned from inline script", value.Type().String())
}

func (w *worker) materializeRequest(ctx context.Context, req *scenario.StageBlueprint, executionTimeout time.Duration) ([]byte, error) {
	if req == nil || len(req.Content) == 0 {
		return []byte{}, nil
	}

	materialized := bytes.NewBuffer(make([]byte, 0, req.Content[len(req.Content)-1].End-req.Content[0].Start))

	for _, token := range req.Content {
		if ctx.Err() != nil {
			return materialized.Bytes(), errors.New("Context cancelled")
		}
		switch token.Type {
		case scenario.Text:
			materialized.Write(req.ExtractPayload(token))
		case scenario.Script:
			value, err := w.executeAndExtractValue(token, req.ExtractPayload, req.Filename, executionTimeout)
			if err != nil {
				return []byte{}, err
			}
			result, err := w.materializeLValue(value)
			if err != nil {
				return []byte{}, fmt.Errorf("%s#%d:%d: Failed to materialize request: %w", req.Filename, token.Line, token.Offset, err)
			}
			materialized.WriteString(result)
		case scenario.IncompleteToken:
			return []byte{}, fmt.Errorf("%s#%d:%d: Failed to materialize request: incomplete token", req.Filename, token.Line, token.Offset)
		case scenario.Delimiter:
			return []byte{}, fmt.Errorf("%s#%d:%d: Failed to materialize request: found delimiter", req.Filename, token.Line, token.Offset)
		case scenario.EOF:
		}
	}

	return materialized.Bytes(), nil
}

func (w *worker) Close() {
	if w.lc != nil {
		w.lc.Close()
	}
}
