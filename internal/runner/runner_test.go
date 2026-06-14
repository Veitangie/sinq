// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
	"go.uber.org/goleak"
)

type noopRoundTripper struct{}

func (r noopRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestNewRunner_Validation(t *testing.T) {
	cfg := config.SaneDefaults()

	_, err := NewRunner(cfg, context.Background(), nil, *slog.Default(), nil)
	if err == nil {
		t.Error("Expected error when creating Runner with nil transport")
	}

	_, err = NewRunner(cfg, nil, noopRoundTripper{}, *slog.Default(), nil)
	if err == nil {
		t.Error("Expected error when creating Runner with nil context")
	}
}

func TestRunner_GoroutineLeakage(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := config.SaneDefaults()
	cfg.Workers = 50

	ctx, cancel := context.WithCancel(context.Background())
	runner, _ := NewRunner(cfg, ctx, noopRoundTripper{}, *slog.Default(), nil)

	scenarios := make([]ScenarioBundle, 1000)
	for i := range scenarios {
		scenarios[i] = ScenarioBundle{scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{Name: "LeakTest"}}, nil}
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
}
