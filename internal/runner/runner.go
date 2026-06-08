// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
)

type Runner struct {
	cfg       config.Config
	ctx       context.Context
	transport http.RoundTripper
	logger    slog.Logger
	clock     Clock
}

func (r *Runner) RunScenarios(ctx context.Context, scenarios []scenario.ScenarioBlueprint, secrets map[string]any) (<-chan ScenarioResult, <-chan time.Duration, <-chan error) {

	taskCh := make(chan scenario.ScenarioBlueprint, r.cfg.Workers)
	errorCh := make(chan error, r.cfg.Workers)
	resultCh := make(chan ScenarioResult, r.cfg.Workers)
	durationCh := make(chan time.Duration, 1)
	wg := sync.WaitGroup{}

	go func() {
	Loop:
		for _, sc := range scenarios {
			select {
			case <-ctx.Done():
				break Loop
			case taskCh <- sc:
			}
		}
		close(taskCh)
	}()

	sharedCache := map[scriptKey]*lua.FunctionProto{}
	sharedLock := sync.RWMutex{}

	go func() {
		timer := newTimer(r.clock)
		timer.start()
		defer func() {
			close(errorCh)
			close(resultCh)
			close(durationCh)
		}()

		for idx := range r.cfg.Workers {
			w := worker{
				id:                idx,
				secrets:           secrets,
				luaStateHardReset: r.cfg.Safe,
				taskCh:            taskCh,
				errorCh:           errorCh,
				resCh:             resultCh,
				logger:            &r.logger,
				transport:         r.transport,
				wg:                &wg,
				scriptCacheLock:   &sharedLock,
				scriptCache:       sharedCache,
				clock:             r.clock,
			}
			wg.Add(1)

			go w.run(ctx)
		}
		wg.Wait()
		durationCh <- timer.Time()
	}()

	return resultCh, durationCh, errorCh
}

func NewRunner(cfg config.Config, ctx context.Context, transport http.RoundTripper, logger slog.Logger, clock Clock) (*Runner, error) {
	if transport == nil {
		return nil, errors.New("Cannot construct scenario runner: http transport is nil")
	}

	if ctx == nil {
		return nil, errors.New("Cannot construct scenario runner: context is nil")
	}

	if clock == nil {
		clock = DefaultClock{}
	}

	return &Runner{cfg, ctx, transport, logger, clock}, nil
}
