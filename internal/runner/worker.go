// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
)

type Workspace interface {
	fs.StatFS
	Create(string) (io.WriteCloser, error)
}

type workerEnv struct {
	luaStateHardReset bool
	secrets           map[string]any
	logger            *slog.Logger
	transport         http.RoundTripper
	clock             Clock
	compiler          cachedCompiler
	workspace         Workspace
}

type worker struct {
	id                int
	env               workerEnv
	taskCh            <-chan ScenarioBundle
	errorCh           chan<- error
	resCh             chan<- ScenarioResult
	requestIdx        int
	maxRequestIdx     int
	lc                *luaContext
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

func (w *worker) reportResult(ctx context.Context, scenarioTimer ResultTimer, scenarioName string, status ResultStatus, durations []RequestResult) {
	select {
	case w.resCh <- ScenarioResult{
		Name:           scenarioName,
		StartedAt:      scenarioTimer.at,
		TotalDuration:  scenarioTimer.Time(),
		RequestResults: durations,
		Status:         status,
	}:
	case <-ctx.Done():
		w.env.logger.Debug("Failed to publish scenario result because of timeout/cancel", w.loggingContext(ctx)...)
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

			w.processScenario(context.WithValue(ctx, scenarioNameContext, task.Config.Name), task)
		case <-ctx.Done():
			break Scenarios
		}
	}
}

func (w *worker) processRequest(ctx context.Context, scenarioBp *scenario.ScenarioBlueprint, requestIdx int, client *http.Client, status *ResultStatus, result *RequestResult) (error, bool) {
	if ctx.Err() != nil {
		*status = Aborted
		result.Status = Aborted
		return errors.New("Context aborted"), false
	}

	w.lc.setupRequestEnvironment(requestIdx)

	requestTimer := newTimer(w.env.clock)
	processor := RequestProcessor{
		w:                 w,
		ctx:               ctx,
		scenarioBp:        scenarioBp,
		requestBp:         scenarioBp.Requests[requestIdx],
		requestIdx:        requestIdx,
		status:            status,
		result:            result,
		requestTimer:      requestTimer,
		totalRequestTimer: ResultTimer{},
		client:            client,
	}

	if err := processor.runPre(); err != nil {
		return err, false
	}

	if err := processor.materialize(); err != nil {
		return err, false
	}

	if err := processor.parse(); err != nil {
		return err, false
	}

	if err := processor.run(); err != nil {
		return err, false
	}

	if err := processor.runAssert(); err != nil {
		return err, false
	}

	if len(w.assertionFailures) > 0 {
		result.Status = Failure
		result.FailedAssertions = w.assertionFailures
		w.assertionFailures = make([]string, 0)

		if scenarioBp.Config.FailFast {
			return nil, false
		}
	}

	if err := processor.runPost(); err != nil {
		return err, false
	}

	if err := processor.finalize(); err != nil {
		return err, false
	}

	return nil, true
}

func (w *worker) processScenario(ctx context.Context, bundle ScenarioBundle) {
	w.env.logger.Debug("Starting scenario", w.loggingContext(ctx)...)
	requestResults := make([]RequestResult, len(bundle.Requests))
	scenarioTimer := newTimer(w.env.clock)

	jar, err := cookiejar.New(nil)
	if err != nil {
		w.errorCh <- err
		w.env.logger.Warn("Failed to create cookiejar for scenario", w.loggingContextWithErr(ctx, err)...)
		requestResults[0].ErrorMessage = "Failed to create cookie jar"
		w.reportResult(ctx, scenarioTimer, bundle.Config.Name, Aborted, requestResults)
		return
	}

	client := http.Client{
		Transport: w.env.transport,
		Jar:       jar,
		Timeout:   bundle.Config.Timeout.Duration,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= bundle.Config.MaxRedirects {
				return errors.New("Too many redirects")
			}
			return nil
		},
	}

	w.env.workspace = bundle.Workspace
	defer func() {
		w.env.workspace = nil
		if r := recover(); r != nil {
			w.env.logger.Warn("Panic during scenario run", w.loggingContext(ctx)...)
			w.env.logger.Debug("More detailed info on panic", append(w.loggingContext(ctx), "panic", r)...)
			w.reportResult(ctx, scenarioTimer, bundle.Config.Name, Error, requestResults)
		}
	}()

	w.requestIdx = 0
	w.maxRequestIdx = len(bundle.Requests) - 1
	w.assertionFailures = make([]string, 0)
	err = w.setupScenarioEnvironment(ctx, bundle.Config.Env)
	if err != nil {
		w.errorCh <- err
		w.env.logger.Warn("Failed to set up lua environment for scenario", w.loggingContextWithErr(ctx, err)...)
		requestResults[0].ErrorMessage = "Failed to set up lua scenario"
		w.reportResult(ctx, scenarioTimer, bundle.Config.Name, Aborted, requestResults)
		return
	}
	scenarioTimer.start()

	status := Success
	shouldContinue := w.requestIdx >= 0 && w.requestIdx < len(bundle.Requests)
	for shouldContinue {
		oldRequestIdx := w.requestIdx
		currentResult := &requestResults[oldRequestIdx]
		currentResult.Name = bundle.Requests[oldRequestIdx].Filename
		w.requestIdx++

		err, shouldContinue = w.processRequest(
			context.WithValue(ctx, requestNameContext, bundle.Requests[oldRequestIdx].Filename),
			&bundle.ScenarioBlueprint,
			oldRequestIdx,
			&client,
			&status,
			currentResult,
		)
		if err != nil {
			w.env.logger.Debug("Request failed", w.loggingContextWithErr(ctx, err)...)
		}
		shouldContinue = shouldContinue && w.requestIdx < len(bundle.Requests)
		if status == Success && (currentResult.Status == Failure || currentResult.Status == Aborted) {
			status = currentResult.Status
		}
	}

	w.reportResult(ctx, scenarioTimer, bundle.Config.Name, status, requestResults)
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
