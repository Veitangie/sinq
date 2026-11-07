// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.uber.org/goleak"
	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
)

type noopRoundTripper struct{}

func (r noopRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, nil
}

func TestNewRunner_Validation(t *testing.T) {
	cfg := config.SaneDefaults()

	_, err := NewRunner(cfg, context.Background(), nil, newTestBus(t), nil, &mockWorkspace{})
	if err == nil {
		t.Error("Expected error when creating Runner with nil transport")
	}

	_, err = NewRunner(cfg, nil, noopRoundTripper{}, newTestBus(t), nil, &mockWorkspace{})
	if err == nil {
		t.Error("Expected error when creating Runner with nil context")
	}

	_, err = NewRunner(cfg, context.Background(), noopRoundTripper{}, nil, nil, &mockWorkspace{})
	if err == nil {
		t.Error("Expected error when creating Runner with nil event bus")
	}
}

func TestNewRunner_DefaultsNilClock(t *testing.T) {
	cfg := config.SaneDefaults()

	r, err := NewRunner(cfg, context.Background(), noopRoundTripper{}, newTestBus(t), nil, &mockWorkspace{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.clock == nil {
		t.Fatal("expected NewRunner to default a nil clock to timer.DefaultClock{}")
	}
}

func TestRunner_StartDataSource_CountControlsRun(t *testing.T) {
	runner := &Runner{cfg: config.SaneDefaults()}

	scenarios := []ScenarioBundle{
		{Config: &scenario.ScenarioConfig{Name: "ZeroCount"}, Count: 0},
		{Config: &scenario.ScenarioConfig{Name: "OneCount"}, Count: 1},
	}

	taskCh := runner.startDataSource(context.Background(), scenarios)
	tasks := []taskBundle{}
	for task := range taskCh {
		tasks = append(tasks, task)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected exactly 2 tasks (a zero-count scenario still emits one task, just not marked to run), got %d", len(tasks))
	}

	for _, task := range tasks {
		switch task.Config.Name {
		case "ZeroCount":
			if task.run {
				t.Errorf("expected run=false for a scenario with Count=0")
			}
		case "OneCount":
			if !task.run {
				t.Errorf("expected run=true for a scenario with Count=1")
			}
		}
	}
}

func TestRunner_GoroutineLeakage(t *testing.T) {
	defer goleak.VerifyNone(t)

	cfg := config.SaneDefaults()
	cfg.Workers = 50

	bus, err := event.NewBus(64, discardLogger())
	if err != nil {
		t.Fatalf("Failed to construct event bus: %v", err)
	}
	bus.Start()
	resultCh, durationCh := aggregator.NewAggregator().AggregateOver(bus.RegisterMC(cfg.Workers * 2))

	ctx, cancel := context.WithCancel(context.Background())
	runner, _ := NewRunner(cfg, ctx, noopRoundTripper{}, bus, nil, &mockWorkspace{})

	scenarios := make([]ScenarioBundle, 1000)
	for i := range scenarios {
		scenarios[i] = ScenarioBundle{scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{Name: "LeakTest"}}, nil, 1}
	}

	totalTimer := timer.NewTimer(timer.DefaultClock{})
	totalTimer.Start()
	runner.RunScenarios(ctx, scenarios, nil, &totalTimer)

	for range 10 {
		<-resultCh
	}

	cancel()

	go func() {
		for range resultCh {
		}
	}()
	select {
	case <-durationCh:
	case <-time.After(5 * time.Second):
		t.Error("Timed out waiting for the runner to finish after cancel (RunnerDone never reached the aggregator)")
	}
	bus.Close()
}

func TestRunner_RunnerDoneIsSentAfterCancel(t *testing.T) {
	for i := range 20 {
		bus, err := event.NewBus(64, discardLogger())
		if err != nil {
			t.Fatalf("Failed to construct event bus: %v", err)
		}
		sink := bus.RegisterMC(64)
		bus.Start()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		runner, _ := NewRunner(config.SaneDefaults(), ctx, noopRoundTripper{}, bus, nil, &mockWorkspace{})
		totalTimer := timer.NewTimer(timer.DefaultClock{})
		runner.RunScenarios(ctx, []ScenarioBundle{}, nil, &totalTimer)

		gotDone := false
		timeout := time.After(time.Second)
	Wait:
		for !gotDone {
			select {
			case e := <-sink:
				gotDone = e.Kind == event.RunnerDone
			case <-timeout:
				break Wait
			}
		}
		bus.Close()
		if !gotDone {
			t.Fatalf("Run %d: RunnerDone was dropped because ctx was already cancelled", i+1)
		}
	}
}

func TestRunner_StartDataSource_RunIdsAreUnique(t *testing.T) {
	runner := &Runner{cfg: config.SaneDefaults()}
	scenarios := []ScenarioBundle{
		{
			SeqId: 1,
			Config: &scenario.ScenarioConfig{
				Name:      "Matrix",
				EnvMatrix: []map[string]map[string]any{{"a": {}, "b": {}}, {"x": {}, "y": {}}},
			},
			Count: 3,
		},
		{SeqId: 2, Config: &scenario.ScenarioConfig{Name: "Plain"}, Count: 2},
	}

	seen := map[event.RunId]bool{}
	n := 0
	for task := range runner.startDataSource(context.Background(), scenarios) {
		n++
		if seen[task.runId] {
			t.Errorf("RunId %+v was given to more than one task", task.runId)
		}
		seen[task.runId] = true
		if task.runId.SeqId != task.SeqId {
			t.Errorf("Expected RunId.SeqId %d to match the blueprint's SeqId %d", task.runId.SeqId, task.SeqId)
		}
	}
	if n != 4*3+2 {
		t.Errorf("Expected 14 tasks (2x2 matrix x3 + 2), got %d", n)
	}
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

		if task.Config.Name != "MatrixCheckout" {
			t.Errorf("Expected the task to keep the base name, got %q", task.Config.Name)
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
			Config: &scenario.ScenarioConfig{
				Name: "PlainScenario",
				Env: map[string]any{
					"key": "value",
				},
				EnvMatrix: nil,
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
	if task.Config.Name != "PlainScenario" {
		t.Errorf("Expected the base name to be unchanged, got %q", task.Config.Name)
	}
	if task.env["key"] != "value" {
		t.Errorf("Env map was corrupted in standard execution")
	}
}
