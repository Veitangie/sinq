// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
)

func TestWorker_ExecuteAndExtractValue_CacheTrap(t *testing.T) {
	w := &worker{
		scriptCacheLock: &sync.RWMutex{},
		scriptCache:     make(map[scriptKey]*lua.FunctionProto),
		wg:              &sync.WaitGroup{},
	}
	w.setupLuaEnvironment(context.Background(), nil)
	w.wg.Add(1)
	defer w.Close()

	scriptPayload := []byte(`"hello world"`)
	token := scenario.Token{
		Type: scenario.Script,
		Name: "TEST_SCRIPT",
	}

	extractFunc := func(scenario.Token) []byte {
		return scriptPayload
	}

	val, err := w.executeAndExtractValue(token, extractFunc, "test_file.sinq", 2*time.Second)
	if err != nil {
		t.Fatalf("Cache Trap Triggered: Expected no error, got: %v", err)
	}

	if val.String() != "hello world" {
		t.Errorf("Expected 'hello world', got: %v", val.String())
	}
}

func TestWorker_RequestCompleted_Indexing(t *testing.T) {
	w := &worker{
		scriptCacheLock: &sync.RWMutex{},
		scriptCache:     make(map[scriptKey]*lua.FunctionProto),
		wg:              &sync.WaitGroup{},
		logger:          slog.Default(),
	}
	w.setupLuaEnvironment(context.Background(), nil)
	w.wg.Add(1)
	defer w.Close()

	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"status": "ok"}`))),
	}

	err := w.requestCompleted(context.Background(), resp, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = w.ls.DoString(`
		if sinq.responses[1] == nil then
			error("Request not found at index 1. Go passed 0-index directly to Lua.")
		end
		if sinq.responses[1].code ~= 200 then
			error("Expected code 200")
		end
	`)
	if err != nil {
		t.Fatalf("Indexing bug triggered: %v", err)
	}
}

func TestWorker_RequestCompleted_JSONArrayBlindspot(t *testing.T) {
	w := &worker{
		scriptCacheLock: &sync.RWMutex{},
		scriptCache:     make(map[scriptKey]*lua.FunctionProto),
		wg:              &sync.WaitGroup{},
		logger:          slog.Default(),
	}
	w.setupLuaEnvironment(context.Background(), nil)
	w.wg.Add(1)
	defer w.Close()

	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader([]byte(`[{"id": 1}, {"id": 2}]`))),
	}

	err := w.requestCompleted(context.Background(), resp, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = w.ls.DoString(`
		local req = sinq.responses[1]
		if req.body == nil then
			error("Body is nil. JSON array parsing silently failed in Go.")
		end
		if req.body[1].id ~= 1 then
			error("Expected first element id to be 1")
		end
	`)
	if err != nil {
		t.Fatalf("JSON Array parsing bug triggered: %v", err)
	}
}

func TestWorker_ContextCancellation_CleanExit(t *testing.T) {
	taskCh := make(chan scenario.ScenarioBlueprint)
	errorCh := make(chan error, 1)
	resCh := make(chan ScenarioResult, 1)

	ctx, cancel := context.WithCancel(context.Background())

	w := &worker{
		id:      1,
		taskCh:  taskCh,
		errorCh: errorCh,
		resCh:   resCh,
		wg:      &sync.WaitGroup{},
	}
	w.wg.Add(1)

	go w.run(ctx)

	cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Worker deadlocked and failed to exit upon context cancellation")
	}
}
