// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
)

func TestRequestProcessor_ContextCancellationDuringRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	w := &worker{
		id:              1,
		logger:          slog.Default(),
		scriptCacheLock: &sync.RWMutex{},
		scriptCache:     make(map[scriptKey]*lua.FunctionProto),
		wg:              &sync.WaitGroup{},
		clock:           DefaultClock{},
	}
	err := w.setupLuaEnvironment(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to setup Lua environment: %v", err)
	}
	w.wg.Add(1)
	defer w.Close()

	rawSinq := "GET " + srv.URL + "\n$RETRY{\n return 10000 \n}"
	reqBp, err := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "retry_test.sinq")
	if err != nil {
		t.Fatalf("Failed to parse blueprint: %v", err)
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{
			MaxRetries: 3,
			Timeout:    scenario.Duration{Duration: 30 * time.Second},
		},
	}

	status := Success
	result := &RequestResult{}

	processor := RequestProcessor{
		w:            w,
		ctx:          ctx,
		scenarioBp:   scenarioBp,
		requestBp:    reqBp,
		status:       &status,
		result:       result,
		requestTimer: newTimer(DefaultClock{}),
		client:       srv.Client(),
	}

	_ = processor.materialize()
	_ = processor.parse()

	done := make(chan error)
	go func() {
		done <- processor.run()
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil || err.Error() != "Context aborted while waiting for retry" {
			t.Fatalf("Expected context abort error, got: %v", err)
		}
		if *processor.status != Aborted {
			t.Errorf("Expected processor status to be Aborted, got %v", *processor.status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("RequestProcessor ignored context cancellation and deadlocked in retry loop")
	}
}
