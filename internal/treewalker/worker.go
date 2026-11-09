// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package treewalker

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"veitangie.dev/sinq/internal/envs"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
)

type workerEnv struct {
	id                      int
	stageCache              map[string][]*scenario.StageBlueprint
	scenarioConfigCache     map[string]scenario.ScenarioConfig
	stageCacheLock          *sync.RWMutex
	scenarioConfigCacheLock *sync.RWMutex
	t                       *Treewalker
	seqId                   int
	seq                     int
}

type worker struct {
	workerEnv
	taskCh     <-chan task
	errorCh    chan<- error
	resultCh   chan<- scenario.ScenarioBlueprint
	fileSystem fs.FS
	wg         *sync.WaitGroup
}

func (w *worker) log(level slog.Level, msg string, attrs ...slog.Attr) event.Event {
	e := newEvent(event.Log, w.id)
	e.Level = level
	e.Message = msg
	e.Attrs = append([]slog.Attr{slog.Int("seqId", w.seqId)}, attrs...)
	return e
}

func (w *worker) send(es ...event.Event) {
	for i := range es {
		w.seq++
		es[i].WorkerSeq = w.seq
	}
	w.t.eb.Send(context.Background(), es...)
}

func (w *worker) run(ctx context.Context) {
	defer w.wg.Done()

	for task := range w.taskCh {
		if ctx.Err() != nil {
			break
		}
		w.runTask(task)
	}
}

func (w *worker) runTask(t task) {
	w.seqId = t.seqId
	w.send(w.log(slog.LevelDebug, "[Treewalker] Worker got task", slog.Any("taskPaths", t.paths)))

	scenarioConfig := scenario.SaneDefaultConfig()
	stageBlueprints := make([]*scenario.StageBlueprint, 0, len(t.paths))
	for _, filePath := range t.paths {
		switch {
		case strings.HasSuffix(filePath, stagesFiletype):
			w.handleStageFile(&stageBlueprints, filePath, t.rootName)
		case strings.HasSuffix(filePath, scenarioConfigFiletype):
			w.handleScenarioConfigFile(&scenarioConfig, filePath, t.rootName)
		}
	}

	if len(stageBlueprints) == 0 {
		return
	}

	if scenarioConfig.Name == "" {
		scenarioConfig.Name = getDefaultScenarioNameFromPath(t.paths[len(t.paths)-1], t.rootName, w.t.cwdName)
	}

	if len(scenarioConfig.Tags) > 0 {
		scenarioConfig.TagsList = make([]string, 0, len(scenarioConfig.Tags))
		for tag := range scenarioConfig.Tags {
			scenarioConfig.TagsList = append(scenarioConfig.TagsList, tag)
		}
	}

	envs.DeepMerge(scenarioConfig.Env, w.t.cfg.Treewalker.Env)

	res := scenario.ScenarioBlueprint{SeqId: t.seqId, Stages: stageBlueprints, Config: &scenarioConfig}
	w.resultCh <- res
}

func getDefaultScenarioNameFromPath(lastFilePath, rootPath, cwdPath string) string {
	name := filepath.Dir(lastFilePath)
	if name == "." {
		name = filepath.Base(rootPath)
	}
	if name == "." {
		name = filepath.Base(cwdPath)
	}
	return name
}

func (t *Treewalker) runWorkers(ctx context.Context, fileSystem fs.FS, taskCh <-chan task, errorCh chan<- error) (*sync.WaitGroup, chan scenario.ScenarioBlueprint) {
	wg := sync.WaitGroup{}
	wg.Add(t.cfg.Workers)
	resultCh := make(chan scenario.ScenarioBlueprint)

	for _, wEnv := range t.workerEnvs {
		w := worker{
			workerEnv:  wEnv,
			fileSystem: fileSystem,
			taskCh:     taskCh,
			errorCh:    errorCh,
			resultCh:   resultCh,
			wg:         &wg,
		}
		go w.run(ctx)
	}

	go func() {
		defer close(resultCh)
		wg.Wait()
	}()

	return &wg, resultCh
}

func (w *worker) handleScenarioConfigFile(scenarioConfig *scenario.ScenarioConfig, filePath, rootName string) {
	fullFilePath := filepath.Join(rootName, filePath)
	if cachedScenarioConfig, isFound := readCache(fullFilePath, w.scenarioConfigCache, w.scenarioConfigCacheLock); isFound {
		*scenarioConfig = cachedScenarioConfig
		scenarioConfig.Env = envs.DeepCopy(cachedScenarioConfig.Env)
		scenarioConfig.EnvMatrix = slices.Clone(cachedScenarioConfig.EnvMatrix)
		scenarioConfig.Tags = maps.Clone(cachedScenarioConfig.Tags)
		scenarioConfig.TagsList = slices.Clone(cachedScenarioConfig.TagsList)
		return
	}

	file, err := w.fileSystem.Open(filePath)
	if err != nil {
		w.send(w.log(slog.LevelDebug, "[Treewalker] Error occurred while opening file", event.ErrAttr(err), slog.String("filePath", fullFilePath)))
		w.errorCh <- located(fullFilePath, fmt.Errorf("Failed to open file: %w", err))
		return
	}
	defer file.Close()

	err = w.t.parseScenarioConfig(scenarioConfig, file)
	if err != nil {
		w.send(w.log(slog.LevelDebug, "[Treewalker] Error occurred while parsing file", event.ErrAttr(err), slog.String("filePath", fullFilePath)))
		w.errorCh <- located(fullFilePath, err)
		return
	}

	newConfig := *scenarioConfig
	newConfig.Env = envs.DeepCopy(scenarioConfig.Env)
	newConfig.EnvMatrix = slices.Clone(scenarioConfig.EnvMatrix)
	newConfig.Tags = maps.Clone(scenarioConfig.Tags)
	newConfig.TagsList = slices.Clone(scenarioConfig.TagsList)
	updateCache(fullFilePath, newConfig, w.scenarioConfigCache, w.scenarioConfigCacheLock)
}

func (w *worker) handleStageFile(stageBlueprints *[]*scenario.StageBlueprint, filePath, rootName string) {

	fullFilePath := filepath.Join(rootName, filePath)
	if cachedStages, isFound := readCache(fullFilePath, w.stageCache, w.stageCacheLock); isFound {
		w.send(w.log(slog.LevelDebug, "[Treewalker] There was a cache hit", slog.String("filePath", fullFilePath)))
		for _, cached := range cachedStages {
			bp := *cached
			bp.Filename = filePath
			bp.Rootname = rootName
			*stageBlueprints = append(*stageBlueprints, &bp)
		}
		return
	}

	file, err := w.fileSystem.Open(filePath)
	if err != nil {
		w.send(w.log(slog.LevelDebug, "[Treewalker] Error occurred while opening file", event.ErrAttr(err), slog.String("filePath", fullFilePath)))
		w.errorCh <- located(fullFilePath, fmt.Errorf("Failed to open file: %w", err))
		return
	}
	defer file.Close()

	newStageBlueprints, err := w.t.parseStages(file, filePath, rootName)
	if err != nil {
		w.send(w.log(slog.LevelDebug, "[Treewalker] Error occurred while parsing file", event.ErrAttr(err), slog.String("filePath", fullFilePath)))
		w.errorCh <- located(fullFilePath, err)
		return
	}

	*stageBlueprints = append(*stageBlueprints, newStageBlueprints...)

	updateCache(fullFilePath, newStageBlueprints, w.stageCache, w.stageCacheLock)
}

func readCache[K comparable, V any](k K, cache map[K]V, lock *sync.RWMutex) (V, bool) {
	lock.RLock()
	v, ok := cache[k]
	lock.RUnlock()
	return v, ok
}

func updateCache[K comparable, V any](k K, v V, cache map[K]V, lock *sync.RWMutex) {
	lock.Lock()
	cache[k] = v
	lock.Unlock()
}
