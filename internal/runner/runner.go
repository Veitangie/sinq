// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"errors"
	"hash/maphash"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
	"golang.org/x/sync/singleflight"
)

type Runner struct {
	cfg           config.Config
	ctx           context.Context
	transport     http.RoundTripper
	logger        slog.Logger
	clock         timer.Clock
	rootWorkspace Workspace
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

func (r *Runner) RunScenarios(ctx context.Context, scenarios []ScenarioBundle, secrets map[string]any, totalTimer *timer.Timer) (<-chan ScenarioResult, <-chan time.Duration, <-chan error) {

	taskCh := r.startDataSource(ctx, scenarios)
	errorCh := make(chan error, r.cfg.Workers)
	resultCh := make(chan ScenarioResult, r.cfg.Workers)
	durationCh := make(chan time.Duration, 1)
	wg := sync.WaitGroup{}
	singleflightProcessor := cachedRequestProcessor{
		group:        singleflight.Group{},
		ctx:          ctx,
		transport:    r.transport,
		maxCacheSize: r.cfg.MaxCacheSize,
		cacheTimeout: r.cfg.CacheTimeout,
		logger:       &r.logger,
	}

	loader := cachedLoader{
		path:      r.cfg.LuaPaths,
		workspace: r.rootWorkspace,
		group:     singleflight.Group{},
	}

	sharedSeed := maphash.MakeSeed()

	go func() {
		defer func() {
			close(errorCh)
			close(resultCh)
			close(durationCh)
		}()

		compiler := cachedCompiler{
			group:      singleflight.Group{},
			cache:      sync.Map{},
			hasherSeed: sharedSeed,
		}

		hasher := maphash.Hash{}
		hasher.SetSeed(sharedSeed)
		env := workerEnv{
			cfg:             r.cfg,
			secrets:         secrets,
			logger:          &r.logger,
			transport:       r.transport,
			clock:           r.clock,
			compiler:        &compiler,
			cachedProcessor: &singleflightProcessor,
			hasher:          hasher,
			loader:          &loader,
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
		durationCh <- totalTimer.Time()
	}()

	return resultCh, durationCh, errorCh
}

func NewRunner(cfg config.Config, ctx context.Context, transport http.RoundTripper, logger slog.Logger, clock timer.Clock, rootWorkspace Workspace) (*Runner, error) {
	if transport == nil {
		return nil, errors.New("Cannot construct scenario runner: http transport is nil")
	}

	if ctx == nil {
		return nil, errors.New("Cannot construct scenario runner: context is nil")
	}

	if clock == nil {
		clock = timer.DefaultClock{}
	}

	if rootWorkspace == nil {
		return nil, errors.New("Cannot construct scenario runner: root workspace is nil")
	}

	return &Runner{cfg, ctx, transport, logger, clock, rootWorkspace}, nil
}
