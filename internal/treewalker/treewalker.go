// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package treewalker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/envs"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/natsort"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
)

type TreewalkerCtxKey string

const (
	PathCtxKey TreewalkerCtxKey = "path"
)

type ParseStagesFunc func(io.Reader, string, string) ([]*scenario.StageBlueprint, error)
type ParseScenarioConfigFunc func(*scenario.ScenarioConfig, io.Reader) error

type task struct {
	paths    []string
	rootName string
	seqId    int
}

type Treewalker struct {
	cfg                 config.Config
	eb                  *event.Bus
	parseStages         ParseStagesFunc
	parseScenarioConfig ParseScenarioConfigFunc
	seqId               *atomic.Int64
	eventSeq            *atomic.Int64
	workerEnvs          []workerEnv
	cwdName             string
}

func NewTreewalker(cfg config.Config, eb *event.Bus, parseStages ParseStagesFunc, parseScenarioConfig ParseScenarioConfigFunc, cwdName string) (*Treewalker, error) {
	if parseStages == nil {
		return nil, errors.New("empty parse stage function")
	}
	if parseScenarioConfig == nil {
		return nil, errors.New("empty parse scenario config function")
	}
	if eb == nil {
		return nil, errors.New("empty EventBus")
	}
	if cfg.Workers <= 0 {
		return nil, fmt.Errorf("worker count non-positive: %d", cfg.Workers)
	}
	stageCache := make(map[string][]*scenario.StageBlueprint)
	stageCacheLock := sync.RWMutex{}
	scenarioConfigCache := make(map[string]scenario.ScenarioConfig)
	scenarioConfigCacheLock := sync.RWMutex{}

	res := &Treewalker{cfg, eb, parseStages, parseScenarioConfig, &atomic.Int64{}, &atomic.Int64{}, make([]workerEnv, 0, cfg.Workers), cwdName}
	for wIdx := range cfg.Workers {
		res.workerEnvs = append(res.workerEnvs, workerEnv{
			id:                      wIdx + 1,
			stageCache:              stageCache,
			scenarioConfigCache:     scenarioConfigCache,
			stageCacheLock:          &stageCacheLock,
			scenarioConfigCacheLock: &scenarioConfigCacheLock,
			t:                       res,
		})
	}
	return res, nil
}

const coordinatorId = 0

func newEvent(kind event.EventType, workerId int) event.Event {
	return event.Event{
		Kind:     kind,
		WorkerId: workerId,
		Source:   event.Treewalker,
	}
}

func (t *Treewalker) log(level slog.Level, msg string, attrs ...slog.Attr) event.Event {
	e := newEvent(event.Log, coordinatorId)
	e.Level = level
	e.Message = msg
	e.Attrs = attrs
	return e
}

func located(path string, err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		errs := joined.Unwrap()
		res := make([]error, 0, len(errs))
		for _, e := range errs {
			res = append(res, located(path, e))
		}
		return errors.Join(res...)
	}

	if posErr, ok := err.(scenario.PositionError); ok {
		return fmt.Errorf("%s:%d:%d: %w", path, posErr.Line, posErr.Offset, posErr.Err)
	}
	return fmt.Errorf("%s: %w", path, err)
}

func (t *Treewalker) send(es ...event.Event) {
	for i := range es {
		es[i].WorkerSeq = int(t.eventSeq.Add(1))
	}
	t.eb.Send(context.Background(), es...)
}

const stagesFiletype string = ".sinq"
const scenarioConfigFiletype string = ".scenario"

func (t *Treewalker) nextSeqId() (seqId int) {
	seqId = int(t.seqId.Add(1))
	return
}

func (t *Treewalker) exploreFS(ctx context.Context, cancelCtx context.CancelCauseFunc, directoryName string, fileSystem fs.FS, taskCh chan<- task, errorCh chan<- error, toPrepend []string, rootName string) {

	if ctx.Err() != nil {
		return
	}

	entries, err := fs.ReadDir(fileSystem, directoryName)
	if err != nil {
		t.send(t.log(slog.LevelDebug, "[Treewalker] An error occurred while exploring the file system", event.ErrAttr(err), slog.String("directory", directoryName)))
		err = located(filepath.Join(rootName, directoryName), fmt.Errorf("Failed to read directory: %w", err))
		select {
		case errorCh <- err:
		case <-ctx.Done():
		}
		return
	}

	slices.SortFunc(entries, func(entryOne, entryTwo fs.DirEntry) int {
		if entryOne.Name() == entryTwo.Name() {
			return 0
		}
		if natsort.Compare(entryOne.Name(), entryTwo.Name()) {
			return -1
		}
		return 1
	})

	dirs := make([]string, 0, len(entries))
	oldLen := len(toPrepend)
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
			continue
		}

		if entry.Type().IsRegular() && (strings.HasSuffix(entry.Name(), stagesFiletype) || strings.HasSuffix(entry.Name(), scenarioConfigFiletype)) {
			toPrepend = append(toPrepend, path.Join(directoryName, entry.Name()))
		}
	}

	if len(dirs) == 0 {
		if len(toPrepend) != oldLen {
			select {
			case taskCh <- task{
				paths:    toPrepend,
				seqId:    t.nextSeqId(),
				rootName: rootName,
			}:
			case <-ctx.Done():
			}
		}
		return
	}

	for _, dir := range dirs {
		t.exploreFS(ctx, cancelCtx, path.Join(directoryName, dir), fileSystem, taskCh, errorCh, slices.Clone(toPrepend), rootName)
	}
}

func (t *Treewalker) startExploration(ctx context.Context, cancelCtx context.CancelCauseFunc, fileSystem fs.FS, errorCh chan<- error, wg *sync.WaitGroup, rootName string) <-chan task {
	taskCh := make(chan task, t.cfg.Workers)
	go func() {
		defer func() {
			close(taskCh)
			wg.Done()
		}()
		t.exploreFS(ctx, cancelCtx, ".", fileSystem, taskCh, errorCh, []string{}, rootName)
	}()
	return taskCh
}

func (t *Treewalker) ParseSingleFile(fileSystem fs.FS, filename, rootName string) (scenario.ScenarioBlueprint, error) {
	totalTimer := timer.NewTimer(timer.DefaultClock{})
	defer func() {
		t.send(t.discoveryFinished(filename, totalTimer))
	}()

	res := scenario.ScenarioBlueprint{}
	base := filepath.Base(filename)
	file, err := fileSystem.Open(base)
	if err != nil {
		return res, located(filepath.Join(rootName, base), fmt.Errorf("Failed to open file: %w", err))
	}

	stages, err := t.parseStages(file, base, rootName)
	file.Close()
	if err != nil {
		return res, located(filepath.Join(rootName, base), err)
	}

	res.Stages = stages

	cfg := scenario.SaneDefaultConfig()
	cfg.Name = base
	envs.DeepMerge(cfg.Env, t.cfg.Treewalker.Env)

	res.Config = &cfg
	res.SeqId = t.nextSeqId()

	return res, nil
}

func (t *Treewalker) ParseFiletree(ctx context.Context, fileSystem fs.FS, rootName string) ([]scenario.ScenarioBlueprint, error) {
	totalTimer := timer.NewTimer(timer.DefaultClock{})
	errorCh := make(chan error, t.cfg.Workers)

	cancellableCtx, cancelCtx := context.WithCancelCause(ctx)
	defer cancelCtx(nil)
	coordinatorWG := sync.WaitGroup{}
	coordinatorWG.Add(3)

	taskCh := t.startExploration(cancellableCtx, cancelCtx, fileSystem, errorCh, &coordinatorWG, rootName)

	workersWG, resultCh := t.runWorkers(cancellableCtx, fileSystem, taskCh, errorCh)

	go func() {
		defer close(errorCh)
		workersWG.Wait()
	}()

	var err error
	go func() {
		defer coordinatorWG.Done()

		allErr := make([]error, 0)
		for err := range errorCh {
			allErr = append(allErr, err)
		}

		err = errors.Join(allErr...)
	}()

	res := make([]scenario.ScenarioBlueprint, 0)
	go func() {
		defer coordinatorWG.Done()

		for scenarioBP := range resultCh {
			res = append(res, scenarioBP)
		}
	}()

	coordinatorWG.Wait()
	if ctxErr := context.Cause(cancellableCtx); ctxErr != nil {
		err = errors.Join(err, ctxErr)
	}
	root, _ := ctx.Value(PathCtxKey).(string)
	t.send(t.discoveryFinished(root, totalTimer))
	return res, err
}

func (t *Treewalker) discoveryFinished(root string, totalTimer timer.Timer) event.Event {
	return t.log(
		slog.LevelDebug,
		"[Treewalker] Finished discovery",
		slog.String("path", root),
		slog.Time("startedAt", totalTimer.StartedAt()),
		slog.Duration("duration", totalTimer.Time()),
	)
}

func (t *Treewalker) ParseSecrets() (map[string]any, error) {
	secrets := make(map[string]any, 0)
	if len(t.cfg.Treewalker.SecretsFile) > 0 {
		bytes, err := os.ReadFile(t.cfg.Treewalker.SecretsFile)
		if err != nil {
			t.send(t.log(slog.LevelDebug, "[Treewalker] Failed to read file to parse secrets", event.ErrAttr(err)))
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("Secrets file %s does not exist", t.cfg.Treewalker.SecretsFile)
			}
			return nil, errors.New("Failed to read secrets")
		}

		if len(bytes) > 0 {
			err = json.Unmarshal(bytes, &secrets)
			if err != nil {
				t.send(t.log(slog.LevelDebug, "[Treewalker] Failed to unmarshal secrets", event.ErrAttr(err)))
				return nil, errors.New("Failed to unmarshal secrets")
			}
		}
	}

	envs.DeepMerge(secrets, t.cfg.Treewalker.Secrets)

	return secrets, nil
}
