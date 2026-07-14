// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/Veitangie/sinq/internal/timer"
	lua "github.com/yuin/gopher-lua"
)

type mockWorkspace struct {
	fs.FS
}

func (m *mockWorkspace) Open(name string) (fs.File, error) {
	return m.FS.Open(name)
}

func (m *mockWorkspace) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(m.FS, name)
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (m *mockWorkspace) Create(name string) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	return nopWriteCloser{buf}, nil
}

func setupTestWorker(t *testing.T, ctx context.Context) *worker {
	t.Helper()

	if ctx == nil {
		ctx = context.Background()
	}

	sharedCache := make(map[scriptKey]*lua.FunctionProto)
	sharedLock := &sync.RWMutex{}

	env := workerEnv{
		logger:    slog.Default(),
		clock:     timer.DefaultClock{},
		transport: http.DefaultTransport,
		compiler: cachedCompiler{
			scriptCacheLock: sharedLock,
			scriptCache:     sharedCache,
		},
		workspace: &mockWorkspace{FS: fstest.MapFS{}},
	}

	w := &worker{
		id:  1,
		env: env,
	}

	err := w.setupScenarioEnvironment(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to setup Lua environment: %v", err)
	}

	t.Cleanup(func() {
		w.Close()
	})

	return w
}
