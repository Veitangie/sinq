// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"hash/maphash"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/timer"
	"golang.org/x/sync/singleflight"
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

func (m *mockWorkspace) String() string {
	return "test"
}

func setupTestWorker(t *testing.T, ctx context.Context) *worker {
	t.Helper()

	if ctx == nil {
		ctx = context.Background()
	}

	env := workerEnv{
		logger:    slog.Default(),
		clock:     timer.DefaultClock{},
		transport: http.DefaultTransport,
		compiler:  &cachedCompiler{hasherSeed: maphash.MakeSeed()},
		workspace: &mockWorkspace{FS: fstest.MapFS{}},
		cachedProcessor: &cachedRequestProcessor{
			group:        singleflight.Group{},
			ctx:          context.Background(),
			transport:    http.DefaultTransport,
			maxCacheSize: config.DataSize{ByteAmount: 1 << 20, Unit: config.MiByte},
			cacheTimeout: 1 * time.Second,
			logger:       slog.Default(),
		},
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
