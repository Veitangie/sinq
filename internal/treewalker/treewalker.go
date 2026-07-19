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
	"slices"
	"strings"
	"sync"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/natsort"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
)

type TreewalkerCtxKey string

const (
	PathCtxKey TreewalkerCtxKey = "path"
)

type ParseRequestFunc func(io.Reader, string) (*scenario.RequestBlueprint, error)
type ParseScenarioConfigFunc func(*scenario.ScenarioConfig, io.Reader) error

type Treewalker struct {
	cfg                 config.Config
	logger              slog.Logger
	parseRequest        ParseRequestFunc
	parseScenarioConfig ParseScenarioConfigFunc
}

func NewTreewalker(cfg config.Config, logger slog.Logger, parseRequest ParseRequestFunc, parseScenarioConfig ParseScenarioConfigFunc) (*Treewalker, error) {
	if parseRequest == nil {
		return nil, errors.New("Empty parse request function passed to Treewalker")
	}
	if parseScenarioConfig == nil {
		return nil, errors.New("Empty parse scenario config function passed to Treewalker")
	}
	return &Treewalker{cfg, logger, parseRequest, parseScenarioConfig}, nil
}

const requestFiletype string = ".sinq"
const scenarioConfigFiletype string = ".scenario"

func (t *Treewalker) exploreFS(ctx context.Context, cancelCtx context.CancelCauseFunc, directoryName string, fileSystem fs.FS, taskCh chan<- []string, errorCh chan<- error, toPrepend []string) {

	if ctx.Err() != nil {
		return
	}

	entries, err := fs.ReadDir(fileSystem, directoryName)
	if err != nil {
		t.logger.Error("[Treewalker] An error occurred while exploring the file system", "error", err, "directory", directoryName)
		err = fmt.Errorf("Error on path: %s: %w", directoryName, err)
		select {
		case errorCh <- err:
		case <-ctx.Done():
		}
		if t.cfg.Treewalker.Strict {
			cancelCtx(err)
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

		if entry.Type().IsRegular() && (strings.HasSuffix(entry.Name(), requestFiletype) || strings.HasSuffix(entry.Name(), scenarioConfigFiletype)) {
			toPrepend = append(toPrepend, path.Join(directoryName, entry.Name()))
		}
	}

	if len(dirs) == 0 {
		if len(toPrepend) != oldLen {
			select {
			case taskCh <- toPrepend:
			case <-ctx.Done():
			}
		}
		return
	}

	for _, dir := range dirs {
		t.exploreFS(ctx, cancelCtx, path.Join(directoryName, dir), fileSystem, taskCh, errorCh, slices.Clone(toPrepend))
	}
}

func (t *Treewalker) startExploration(ctx context.Context, cancelCtx context.CancelCauseFunc, fileSystem fs.FS, errorCh chan<- error, wg *sync.WaitGroup) <-chan []string {
	taskCh := make(chan []string, t.cfg.Workers)
	go func() {
		defer func() {
			close(taskCh)
			wg.Done()
		}()
		t.exploreFS(ctx, cancelCtx, ".", fileSystem, taskCh, errorCh, []string{})
	}()
	return taskCh
}

func (t *Treewalker) ParseFiletree(ctx context.Context, fileSystem fs.FS) ([]scenario.ScenarioBlueprint, error) {
	totalTimer := timer.NewTimer(timer.DefaultClock{})
	errorCh := make(chan error, t.cfg.Workers)

	cancellableCtx, cancelCtx := context.WithCancelCause(ctx)
	coordinatorWG := sync.WaitGroup{}
	coordinatorWG.Add(3)

	taskCh := t.startExploration(cancellableCtx, cancelCtx, fileSystem, errorCh, &coordinatorWG)

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
	t.logger.Debug("[Treewalker] Finished discovery", "path", ctx.Value(PathCtxKey), "startedAt", totalTimer.StartedAt(), "duration", totalTimer.Time())
	return res, err
}

func (t *Treewalker) ParseSecrets() (map[string]any, error) {
	secrets := make(map[string]any, 0)
	if len(t.cfg.Treewalker.SecretsFile) > 0 {
		bytes, err := os.ReadFile(t.cfg.Treewalker.SecretsFile)
		if err != nil {
			t.logger.Debug("[Treewalker] Failed to read file to parse secrets", "error", err)
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("Secrets file %s does not exist", t.cfg.Treewalker.SecretsFile)
			}
			return nil, errors.New("Failed to read secrets")
		}

		if len(bytes) > 0 {
			err = json.Unmarshal(bytes, &secrets)
			if err != nil {
				t.logger.Debug("[Treewalker] Failed to unmarshal secrets", "error", err)
				return nil, errors.New("Failed to unmarshal secrets")
			}
		}
	}

	for key, val := range t.cfg.Treewalker.Secret {
		secrets[key] = val
	}

	return secrets, nil
}
