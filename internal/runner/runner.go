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

type ScenarioBundle struct {
	scenario.ScenarioBlueprint
	Workspace Workspace
}

func (r *Runner) startDataSource(ctx context.Context, scenarios []ScenarioBundle) <-chan taskBundle {
	taskCh := make(chan taskBundle, r.cfg.Workers)

	go func() {
	Loop:
		for _, sc := range scenarios {

			allLabels, totalPaths := buildAllPaths(sc.Config.EnvMatrix)

			for path := range totalPaths {

				labels := takePath(path, allLabels)
				totalEnv := deepCopy(sc.Config.Env)
				for idx, label := range labels {
					deepMerge(totalEnv, sc.Config.EnvMatrix[idx][label])
				}

				bundle := taskBundle{
					sc.ScenarioBlueprint,
					sc.Workspace,
					totalEnv,
					labels,
				}

				select {
				case <-ctx.Done():
					break Loop
				case taskCh <- bundle:
				}
			}
		}
		close(taskCh)
	}()

	return taskCh
}

func (r *Runner) RunScenarios(ctx context.Context, scenarios []ScenarioBundle, secrets map[string]any) (<-chan ScenarioResult, <-chan time.Duration, <-chan error) {

	taskCh := r.startDataSource(ctx, scenarios)
	errorCh := make(chan error, r.cfg.Workers)
	resultCh := make(chan ScenarioResult, r.cfg.Workers)
	durationCh := make(chan time.Duration, 1)
	wg := sync.WaitGroup{}

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

		compiler := cachedCompiler{
			scriptCacheLock: &sharedLock,
			scriptCache:     sharedCache,
		}

		env := workerEnv{
			secrets:           secrets,
			luaStateHardReset: r.cfg.Safe,
			logger:            &r.logger,
			transport:         r.transport,
			clock:             r.clock,
			compiler:          compiler,
		}

		for idx := range r.cfg.Workers {
			w := worker{
				id:      idx,
				taskCh:  taskCh,
				errorCh: errorCh,
				resCh:   resultCh,
				env:     env,
			}
			wg.Add(1)

			go func() {
				defer func() {
					w.Close()
					wg.Done()
				}()
				w.run(ctx)
			}()
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
