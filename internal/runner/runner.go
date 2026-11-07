// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"errors"
	"hash/maphash"
	"net/http"
	"sync"

	"golang.org/x/sync/singleflight"
	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/envs"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
)

type Runner struct {
	cfg           config.Config
	ctx           context.Context
	transport     http.RoundTripper
	eb            *event.Bus
	clock         timer.Clock
	rootWorkspace Workspace
}

type ScenarioBundle struct {
	scenario.ScenarioBlueprint
	Workspace Workspace
	Count     uint
}

func (r *Runner) startDataSource(ctx context.Context, scenarios []ScenarioBundle) <-chan taskBundle {
	taskCh := make(chan taskBundle, r.cfg.Workers)

	go func() {
	Loop:
		for _, sc := range scenarios {

			allLabels, totalPaths := buildAllPaths(sc.Config.EnvMatrix)

			for path := range totalPaths {

				labels := takePath(path, allLabels)
				totalEnv := envs.DeepCopy(sc.Config.Env)
				for idx, label := range labels {
					envs.DeepMerge(totalEnv, sc.Config.EnvMatrix[idx][label])
				}

				shouldInclude := sc.Count > 0

				bundle := taskBundle{
					ScenarioBlueprint: sc.ScenarioBlueprint,
					workspace:         sc.Workspace,
					env:               totalEnv,
					labels:            labels,
					run:               shouldInclude,
					runId: event.RunId{
						SeqId: sc.SeqId,
						MatId: path,
						CntId: 0,
					},
				}

				for range max(sc.Count, 1) {
					select {
					case <-ctx.Done():
						break Loop
					case taskCh <- bundle:
					}
					bundle.runId.CntId += 1
				}
			}
		}
		close(taskCh)
	}()

	return taskCh
}

func (r *Runner) RunScenarios(ctx context.Context, scenarios []ScenarioBundle, secrets map[string]any, totalTimer *timer.Timer) {

	taskCh := r.startDataSource(ctx, scenarios)
	wg := sync.WaitGroup{}
	singleflightProcessor := cachedRequestProcessor{
		group:        singleflight.Group{},
		ctx:          ctx,
		transport:    r.transport,
		maxCacheSize: r.cfg.MaxCacheSize,
		cacheTimeout: r.cfg.CacheTimeout,
		eb:           r.eb,
	}

	loader := cachedLoader{
		path:      r.cfg.LuaPaths,
		workspace: r.rootWorkspace,
		group:     singleflight.Group{},
	}

	sharedSeed := maphash.MakeSeed()

	reportContext := context.WithoutCancel(ctx)

	go func() {
		defer func() {
			r.eb.Send(
				reportContext,
				event.Event{
					Kind:     event.RunnerDone,
					Duration: totalTimer.Time(),
				},
			)
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
			eb:              r.eb,
			transport:       r.transport,
			clock:           r.clock,
			compiler:        &compiler,
			cachedProcessor: &singleflightProcessor,
			hasher:          hasher,
			loader:          &loader,
		}

		for idx := range r.cfg.Workers {
			w := worker{
				id:        idx,
				taskCh:    taskCh,
				env:       env,
				reportCtx: reportContext,
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
	}()
}

func NewRunner(cfg config.Config, ctx context.Context, transport http.RoundTripper, eb *event.Bus, clock timer.Clock, rootWorkspace Workspace) (*Runner, error) {
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

	if eb == nil {
		return nil, errors.New("Cannot construct scenario runner: event bus is nil")
	}

	return &Runner{cfg, ctx, transport, eb, clock, rootWorkspace}, nil
}
