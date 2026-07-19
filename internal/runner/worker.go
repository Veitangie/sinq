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
	"strings"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/luapi"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
	"golang.org/x/sync/singleflight"
)

type Workspace interface {
	fs.StatFS
	Create(string) (io.WriteCloser, error)
}

type taskBundle struct {
	scenario.ScenarioBlueprint
	workspace Workspace
	env       map[string]any
	labels    []string
}

func (t taskBundle) getFullName() string {
	if len(t.labels) > 0 {
		return t.Config.Name + "_" + strings.Join(t.labels, "_")
	}
	return t.Config.Name
}

type workerEnv struct {
	cfg          config.Config
	secrets      map[string]any
	luaPath      string
	logger       *slog.Logger
	transport    http.RoundTripper
	clock        timer.Clock
	compiler     cachedCompiler
	workspace    Workspace
	singleFlight *singleflight.Group
	hasher       maphash.Hash
}

type worker struct {
	id                int
	env               workerEnv
	taskCh            <-chan taskBundle
	errorCh           chan<- error
	resCh             chan<- ScenarioResult
	requestIdx        int
	maxRequestIdx     int
	lc                *luapi.LuaContext
	assertionFailures []string
}

type workerContextKey string

const (
	scenarioNameContext workerContextKey = "scenarioName"
	requestNameContext  workerContextKey = "requestName"
)

func (w *worker) loggingContext(ctx context.Context) []any {
	res := make([]any, 0, 8)
	res = append(res, "workerId", w.id)
	scenarioName := ctx.Value(scenarioNameContext)
	if scenarioName != nil {
		res = append(res, string(scenarioNameContext), scenarioName)
	}
	requestName := ctx.Value(requestNameContext)
	if requestName != nil {
		res = append(res, string(requestNameContext), requestName)
	}
	return res
}

func (w *worker) loggingContextWithErr(ctx context.Context, err error) []any {
	return append(w.loggingContext(ctx), "error", err)
}

func (w *worker) reportResult(ctx context.Context, scenarioTimer timer.Timer, result ScenarioResult) {
	result.TotalDuration = scenarioTimer.Time()
	select {
	case w.resCh <- result:
	case <-ctx.Done():
		w.env.logger.Debug("[Runner] Failed to publish scenario result because of timeout/cancel", w.loggingContext(ctx)...)
	}
}

func (w *worker) run(ctx context.Context) {
	if ctx == nil {
		w.errorCh <- errors.New("Worker.run() called with nil context")
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
			w.processScenario(context.WithValue(ctxTimeout, scenarioNameContext, task.getFullName()), task)
			cancel()
		case <-ctx.Done():
			break Scenarios
		}
	}
}

func (w *worker) processRequest(ctx context.Context, scenarioBp *scenario.ScenarioBlueprint, requestIdx int, client *http.Client, status *ResultStatus, result *RequestResult) (bool, error) {
	if ctx.Err() != nil {
		*status = Aborted
		result.Status = Aborted
		return false, errors.New("Context aborted")
	}

	w.lc.SetupRequestEnvironment(requestIdx)

	requestTimer := timer.NewTimer(w.env.clock)
	processor := RequestProcessor{
		w:                 w,
		ctx:               ctx,
		scenarioBp:        scenarioBp,
		requestBp:         scenarioBp.Requests[requestIdx],
		requestIdx:        requestIdx,
		status:            status,
		result:            result,
		requestTimer:      requestTimer,
		totalRequestTimer: timer.NewTimer(w.env.clock),
		client:            client,
	}

	defer processor.finalize()

	if err := processor.runPre(); err != nil {
		return false, err
	}

	if processor.skip {
		result.Status = Aborted
		return true, nil
	}

	if err := processor.materialize(); err != nil {
		return false, err
	}

	if err := processor.doRequest(); err != nil {
		return false, err
	}

	if err := processor.runAssert(); err != nil {
		return false, err
	}

	if len(w.assertionFailures) > 0 {
		result.Status = Failure
		result.FailedAssertions = w.assertionFailures
		w.assertionFailures = make([]string, 0)

		if scenarioBp.Config.FailFast {
			return false, nil
		}
	} else {
		result.Request = ""
		result.Response = ""
	}

	if err := processor.runPost(); err != nil {
		return false, err
	}

	return true, nil
}

func (w *worker) processScenario(ctx context.Context, bundle taskBundle) {
	w.env.logger.Debug("[Runner] Starting scenario", w.loggingContext(ctx)...)
	requestResults := make([]RequestResult, len(bundle.Requests))
	for idx, req := range bundle.Requests {
		requestResults[idx].Name = req.Filename
	}
	scenarioTimer := timer.NewTimer(w.env.clock)
	result := ScenarioResult{
		Name:           bundle.getFullName(),
		Tags:           bundle.Config.TagsList,
		StartedAt:      scenarioTimer.StartedAt(),
		RequestResults: requestResults,
		Status:         Skipped,
	}

	if !w.env.cfg.ShouldInclude(bundle.Config.Tags, bundle.Config.Name) {
		w.reportResult(ctx, scenarioTimer, result)
		return
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		w.errorCh <- err
		w.env.logger.Warn("[Runner] Failed to create cookiejar for scenario", w.loggingContextWithErr(ctx, err)...)
		requestResults[0].ErrorMessage = "Failed to create cookie jar"
		result.Status = Error
		w.reportResult(ctx, scenarioTimer, result)
		return
	}

	client := http.Client{
		Transport: w.env.transport,
		Jar:       jar,
		Timeout:   bundle.Config.ReqTimeout.Duration,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= bundle.Config.MaxRedirects {
				return errors.New("Too many redirects")
			}
			return nil
		},
	}

	w.env.workspace = bundle.workspace
	defer func() {
		w.env.workspace = nil
		if r := recover(); r != nil {
			w.env.logger.Warn("[Runner] Panic during scenario run", w.loggingContext(ctx)...)
			w.env.logger.Debug("[Runner] More detailed info on panic", append(w.loggingContext(ctx), "panic", r)...)
			result.Status = Error
			w.lc.SetContext(ctx)
			w.reportResult(ctx, scenarioTimer, result)
		}
	}()

	w.requestIdx = 0
	w.maxRequestIdx = len(bundle.Requests) - 1
	w.assertionFailures = make([]string, 0)
	err = w.setupScenarioEnvironment(ctx, bundle.env)
	if err != nil {
		w.errorCh <- err
		w.env.logger.Warn("[Runner] Failed to set up lua environment for scenario", w.loggingContextWithErr(ctx, err)...)
		requestResults[0].ErrorMessage = "Failed to set up lua scenario"
		result.Status = Error
		w.reportResult(ctx, scenarioTimer, result)
		return
	}

	status := Success
	shouldContinue := w.requestIdx >= 0 && w.requestIdx < len(bundle.Requests)
	for shouldContinue {
		oldRequestIdx := w.requestIdx
		currentResult := &requestResults[oldRequestIdx]
		w.requestIdx++

		shouldContinue, err = w.processRequest(
			context.WithValue(ctx, requestNameContext, bundle.Requests[oldRequestIdx].Filename),
			&bundle.ScenarioBlueprint,
			oldRequestIdx,
			&client,
			&status,
			currentResult,
		)
		if err != nil {
			w.env.logger.Debug("[Runner] Request failed", w.loggingContextWithErr(ctx, err)...)
		}
		shouldContinue = shouldContinue && w.requestIdx < len(bundle.Requests)
		if status == Success && (currentResult.Status == Failure || currentResult.Status == Aborted) {
			status = currentResult.Status
		}
	}

	result.Status = status
	w.reportResult(ctx, scenarioTimer, result)
}

func (w *worker) materializeRequest(ctx context.Context, req *scenario.RequestBlueprint, executionTimeout time.Duration) ([]byte, error) {
	materialized := bytes.Buffer{}
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
			materialized.Write([]byte(value.String()))
		case scenario.IncompleteToken:
			return []byte{}, fmt.Errorf("%s#%d:%d: Failed to materialize request: incomplete token", req.Filename, token.Line, token.Offset)
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
