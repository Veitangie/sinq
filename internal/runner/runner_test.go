// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
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

	totalTimer := timer.NewTimer(timer.DefaultClock{})
	totalTimer.Start()
	resultCh, durationCh, errorCh := runner.RunScenarios(ctx, scenarios, nil, &totalTimer)

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

func TestRunner_CartesianMath_TakePath(t *testing.T) {
	allLabels := [][]string{
		{"admin", "guest"},
		{"visa", "amex", "mc"},
	}

	expected := [][]string{
		{"admin", "visa"},
		{"admin", "amex"},
		{"admin", "mc"},
		{"guest", "visa"},
		{"guest", "amex"},
		{"guest", "mc"},
	}

	for i := range 6 {
		got := takePath(i, allLabels)
		if len(got) != len(expected[i]) {
			t.Fatalf("takePath(%d) returned length %d, expected %d", i, len(got), len(expected[i]))
		}
		for j := range got {
			if got[j] != expected[i][j] {
				t.Errorf("takePath(%d)[%d] = %s, expected %s", i, j, got[j], expected[i][j])
			}
		}
	}
}

func TestRunner_StartDataSource_MatrixFanOut(t *testing.T) {
	cfg := config.SaneDefaults()
	cfg.Workers = 2

	runner := &Runner{
		cfg: cfg,
	}

	scenarios := []ScenarioBundle{
		{
			ScenarioBlueprint: scenario.ScenarioBlueprint{
				Config: &scenario.ScenarioConfig{
					Name: "MatrixCheckout",
					Env: map[string]any{
						"base_url": "https://api.test",
						"ttl":      30,
					},
					EnvMatrix: []map[string]map[string]any{
						{
							"admin": {"role": "admin", "ttl": 60},
							"guest": {"role": "guest"},
						},
						{
							"success": {"expect": 200},
							"decline": {"expect": 402},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	taskCh := runner.startDataSource(ctx, scenarios)

	var tasks []taskBundle
	for task := range taskCh {
		tasks = append(tasks, task)
	}

	if len(tasks) != 4 {
		t.Fatalf("Expected exactly 4 tasks from matrix fan-out, got %d", len(tasks))
	}

	seenCombinations := make(map[string]bool)

	for _, task := range tasks {
		if len(task.labels) != 2 {
			t.Fatalf("Expected 2 labels per task, got %v", task.labels)
		}

		expectedName := "MatrixCheckout_" + task.labels[0] + "_" + task.labels[1]
		if task.getFullName() != expectedName {
			t.Errorf("getFullName() = %s, expected %s", task.getFullName(), expectedName)
		}

		if task.env["base_url"] != "https://api.test" {
			t.Errorf("Base env leaked or not copied: %v", task.env)
		}

		role := task.env["role"].(string)
		status := task.env["expect"].(int)
		ttl := task.env["ttl"].(int)

		var comboKey string
		if role == "admin" {
			if ttl != 60 {
				t.Errorf("Matrix overwrite failed! Admin should have ttl 60, got %d", ttl)
			}
			comboKey += "admin-"
		} else {
			if ttl != 30 {
				t.Errorf("Base env corrupted! Guest should have ttl 30, got %d", ttl)
			}
			comboKey += "guest-"
		}

		if status == 200 {
			comboKey += "success"
		} else {
			comboKey += "decline"
		}

		seenCombinations[comboKey] = true
	}

	expectedCombos := []string{"admin-success", "admin-decline", "guest-success", "guest-decline"}
	for _, expected := range expectedCombos {
		if !seenCombinations[expected] {
			t.Errorf("Matrix fan-out missed combination: %s", expected)
		}
	}
}

func TestRunner_StartDataSource_NoMatrix(t *testing.T) {
	runner := &Runner{cfg: config.SaneDefaults()}

	scenarios := []ScenarioBundle{
		{
			ScenarioBlueprint: scenario.ScenarioBlueprint{
				Config: &scenario.ScenarioConfig{
					Name: "PlainScenario",
					Env: map[string]any{
						"key": "value",
					},
					EnvMatrix: nil,
				},
			},
		},
	}

	taskCh := runner.startDataSource(context.Background(), scenarios)

	tasks := []taskBundle{}
	for task := range taskCh {
		tasks = append(tasks, task)
	}

	if len(tasks) != 1 {
		t.Fatalf("Expected exactly 1 task for non-matrix scenario, got %d", len(tasks))
	}

	task := tasks[0]
	if len(task.labels) != 0 {
		t.Errorf("Expected 0 labels, got %d", len(task.labels))
	}
	if task.getFullName() != "PlainScenario" {
		t.Errorf("getFullName mutated base name unnecessarily: %s", task.getFullName())
	}
	if task.env["key"] != "value" {
		t.Errorf("Env map was corrupted in standard execution")
	}
}

func TestRunner_GetLuaPath(t *testing.T) {
	cfg := config.SaneDefaults()
	cfg.LuaPaths = []string{"/custom/plugin/path", "./local/plugins"}

	runner := &Runner{cfg: cfg}
	got := runner.getLuaPath()

	p1Base := "/custom/plugin/path"
	p2Base := "local/plugins"

	expectedCrossPlatform := "" +
		filepath.Join(p1Base, "?.lua") + ";" +
		filepath.Join(p1Base, "?", "init.lua") + ";" +
		filepath.Join(p2Base, "?.lua") + ";" +
		filepath.Join(p2Base, "?", "init.lua")

	if got != expectedCrossPlatform {
		t.Errorf("getLuaPath() = %q, want %q", got, expectedCrossPlatform)
	}
}
