// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
)

type noopRoundTripper struct{}

func (r noopRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestRunner_GoroutineLeakage(t *testing.T) {
	runtime.GC()
	time.Sleep(200 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	cfg := config.SaneDefaults()
	cfg.Workers = 50

	ctx, cancel := context.WithCancel(context.Background())
	runner, _ := NewRunner(cfg, ctx, noopRoundTripper{}, *slog.Default(), nil)

	scenarios := make([]scenario.ScenarioBlueprint, 1000)
	for i := range scenarios {
		scenarios[i] = scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{Name: "LeakTest"}}
	}

	resultCh, durationCh, errorCh := runner.RunScenarios(ctx, scenarios, nil)

	for range 10 {
		<-resultCh
	}

	cancel()

	go func() {
		for range resultCh {
		}
	}()
	go func() {
		for range errorCh {
		}
	}()
	<-durationCh

	deadline := time.Now().Add(3 * time.Second)
	leaked := true
	var final int

	for time.Now().Before(deadline) {
		runtime.GC()
		final = runtime.NumGoroutine()

		if final <= baseline+2 {
			leaked = false
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if leaked {
		t.Fatalf("Severe Goroutine leak detected! Started with %d, stuck at %d", baseline, final)
	}
}
