// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package treewalker

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"strings"
	"sync"

	"github.com/Veitangie/sinq/internal/scenario"
)

type worker struct {
	id                      int
	fileSystem              fs.FS
	taskCh                  <-chan []string
	errorCh                 chan<- error
	resultCh                chan<- scenario.ScenarioBlueprint
	requestCache            map[string]*scenario.RequestBlueprint
	scenarioConfigCache     map[string]scenario.ScenarioConfig
	requestCacheLock        *sync.RWMutex
	scenarioConfigCacheLock *sync.RWMutex
	wg                      *sync.WaitGroup
	t                       *Treewalker
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

func (w *worker) runTask(task []string) {
	w.t.logger.Debug("Worker got task", "workerId", w.id, "taskPath", task)

	scenarioConfig := scenario.SaneDefaultConfig()
	requestBlueprints := make([]*scenario.RequestBlueprint, 0, len(task))
	for _, filePath := range task {
		switch {
		case strings.HasSuffix(filePath, requestFiletype):
			w.handleRequestFile(&requestBlueprints, filePath)
		case strings.HasSuffix(filePath, scenarioConfigFiletype):
			w.handleScenarioConfigFile(&scenarioConfig, filePath)
		}
	}

	if scenarioConfig.Name == "" {
		scenarioConfig.Name = getDefaultScenarioNameFromPath(task[len(task)-1])
	}

	res := scenario.ScenarioBlueprint{Requests: requestBlueprints, Config: &scenarioConfig}
	w.resultCh <- res
}

func getDefaultScenarioNameFromPath(lastFilePath string) string {
	idx := strings.LastIndex(lastFilePath, "/")
	if idx < 0 || idx > len(lastFilePath) {
		return lastFilePath
	}
	return strings.Clone(lastFilePath[0:idx])
}

func (t *Treewalker) runWorkers(ctx context.Context, fileSystem fs.FS, taskCh <-chan []string, errorCh chan<- error) (*sync.WaitGroup, chan scenario.ScenarioBlueprint) {
	wg := sync.WaitGroup{}
	wg.Add(t.cfg.Workers)
	requestCache := make(map[string]*scenario.RequestBlueprint)
	requestCacheLock := sync.RWMutex{}
	scenarioConfigCache := make(map[string]scenario.ScenarioConfig)
	scenarioConfigCacheLock := sync.RWMutex{}
	resultCh := make(chan scenario.ScenarioBlueprint)

	for wIdx := range t.cfg.Workers {
		w := worker{
			id:                      wIdx,
			fileSystem:              fileSystem,
			taskCh:                  taskCh,
			errorCh:                 errorCh,
			resultCh:                resultCh,
			requestCache:            requestCache,
			scenarioConfigCache:     scenarioConfigCache,
			requestCacheLock:        &requestCacheLock,
			scenarioConfigCacheLock: &scenarioConfigCacheLock,
			wg:                      &wg,
			t:                       t,
		}
		go w.run(ctx)
	}

	go func() {
		defer close(resultCh)
		wg.Wait()
	}()

	return &wg, resultCh
}

func (w *worker) handleScenarioConfigFile(scenarioConfig *scenario.ScenarioConfig, filePath string) {
	if cachedScenarioConfig, isFound := readCache(filePath, w.scenarioConfigCache, w.scenarioConfigCacheLock); isFound {
		*scenarioConfig = cachedScenarioConfig
		scenarioConfig.Env = maps.Clone(cachedScenarioConfig.Env)
		return
	}

	file, err := w.fileSystem.Open(filePath)
	if err != nil {
		w.t.logger.Error("Error occurred while opening file", "error", err, "filePath", filePath)
		w.errorCh <- fmt.Errorf("Error occurred while opening file %s: %w", filePath, err)
		return
	}
	defer file.Close()

	err = w.t.parseScenarioConfig(scenarioConfig, file)
	if err != nil {
		w.t.logger.Error("Error occurred while parsing file", "error", err, "filePath", filePath)
		w.errorCh <- fmt.Errorf("Error occurred while parsing file %s: %w", filePath, err)
	}

	newConfig := *scenarioConfig
	newConfig.Env = maps.Clone(scenarioConfig.Env)
	updateCache(filePath, newConfig, w.scenarioConfigCache, w.scenarioConfigCacheLock)
}

func (w *worker) handleRequestFile(requestBlueprints *[]*scenario.RequestBlueprint, filePath string) {

	if cachedRequest, isFound := readCache(filePath, w.requestCache, w.requestCacheLock); isFound {
		w.t.logger.Debug("There was a cache hit", "filePath", filePath)
		*requestBlueprints = append(*requestBlueprints, cachedRequest)
		return
	}

	file, err := w.fileSystem.Open(filePath)
	if err != nil {
		w.t.logger.Error("Error occurred while opening file", "error", err, "filePath", filePath)
		w.errorCh <- fmt.Errorf("Error occurred while opening file %s: %w", filePath, err)
		return
	}
	defer file.Close()

	requestBlueprint, err := w.t.parseRequest(file, filePath)
	if err != nil {
		w.t.logger.Error("Error occurred while parsing file", "error", err, "filePath", filePath)
		w.errorCh <- fmt.Errorf("Error occurred while parsing file %s: %w", filePath, err)
		return
	}

	*requestBlueprints = append(*requestBlueprints, requestBlueprint)

	updateCache(filePath, requestBlueprint, w.requestCache, w.requestCacheLock)
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
