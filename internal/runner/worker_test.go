// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/luapi"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
)

func TestWorker_ExecuteAndExtractValue_CacheTrap(t *testing.T) {
	w := setupTestWorker(t, nil)

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
	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	resp := intermediate{
		statusCode: 200,
		headers:    make(http.Header),
		body:       []byte(`{"status": "ok"}`),
	}

	_, err := w.requestCompleted(resp)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = w.lc.DoString(`
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
	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	resp := intermediate{
		statusCode: 200,
		headers:    make(http.Header),
		body:       []byte(`[{"id": 1}, {"id": 2}]`),
	}

	_, err := w.requestCompleted(resp)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	err = w.lc.DoString(`
		local req = sinq.responses[1]
		local bodyJson, err = req.extractBodyJson()
		if err ~= nil then
		  error("Expected successful parse, got " .. err)
		end
		if bodyJson == nil then
			error("Body is nil. JSON array parsing silently failed in Go.")
		end
		if req.bodyJson == nil then
			error("bodyJson is nil. The field has not been set after parse.")
		end
		if req.bodyJson[1].id ~= 1 then
			error("Expected first element id to be 1")
		end
	`)
	if err != nil {
		t.Fatalf("JSON Array parsing bug triggered: %v", err)
	}
}

func TestWorker_ContextCancellation_CleanExit(t *testing.T) {
	taskCh := make(chan taskBundle)
	errorCh := make(chan error, 1)
	resCh := make(chan ScenarioResult, 1)

	ctx, cancel := context.WithCancel(context.Background())

	w := setupTestWorker(t, ctx)
	w.taskCh = taskCh
	w.errorCh = errorCh
	w.resCh = resCh

	done := make(chan struct{})
	go func() {
		w.run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Worker deadlocked and failed to exit upon context cancellation")
	}
}

func TestWorker_ProcessScenario_PanicRecovery(t *testing.T) {
	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)

	w := setupTestWorker(t, nil)
	w.resCh = resCh
	w.errorCh = errorCh

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config: &scenario.ScenarioConfig{
				Name:       "PanicScenario",
				ReqTimeout: scenario.Duration{Duration: 1 * time.Second},
			},
			Requests: []*scenario.RequestBlueprint{{Filename: "req1.sinq"}},
		},
		workspace: nil,
		env:       map[string]any{},
		labels:    []string{},
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != Error {
			t.Errorf("Expected scenario status to be Error due to panic, got %v", res.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Worker did not recover from panic and deadlocked")
	}
}

func TestWorker_SandboxLeak_GlobalG(t *testing.T) {
	w := setupTestWorker(t, nil)

	w.setupScenarioEnvironment(context.Background(), nil)
	token1 := scenario.Token{Type: scenario.Script, Name: "PRE"}
	extract1 := func(scenario.Token) []byte { return []byte(`_G.LEAKED_VAR = "poison"`) }
	w.safeExecute(token1, extract1, "scen1.sinq", 1*time.Second)

	w.setupScenarioEnvironment(context.Background(), nil)
	token2 := scenario.Token{Type: scenario.Script, Name: "PRE"}
	extract2 := func(scenario.Token) []byte {
		return []byte(`if _G.LEAKED_VAR == "poison" then error("LEAK DETECTED") end`)
	}
	err := w.safeExecute(token2, extract2, "scen2.sinq", 1*time.Second)

	if err != nil && strings.Contains(err.Error(), "LEAK DETECTED") {
		t.Fatalf("BUG EXPOSED: _G leaks across scenarios! %v", err)
	}
}

func TestWorker_Unrestricted_FileAccess(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.cfg.Unrestricted = true
	w.lc = luapi.NewLuaContext(timer.DefaultClock{}, w.env.cfg.Unrestricted, nil)

	err := w.lc.DoString(`
		if type(os) ~= "table" then
			error("os table missing in unrestricted mode")
		end
		if type(io) ~= "table" then
			error("io table missing in unrestricted mode")
		end
	`)
	if err != nil {
		t.Fatalf("Unrestricted mode failed: %v", err)
	}
}

func TestWorker_Restricted_NoFileAccess(t *testing.T) {
	w := setupTestWorker(t, nil)

	err := w.lc.DoString(`
		if os ~= nil then
			error("os table should be nil in restricted mode")
		end
		if io ~= nil then
			error("io table should be nil in restricted mode")
		end
	`)
	if err != nil {
		t.Fatalf("Restricted mode failed: %v", err)
	}
}

type mockRoundTripper struct{}

func (r mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200}, nil
}

func TestWorker_ProcessScenario_SkipThenFail(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.transport = mockRoundTripper{}

	skipBP, err := scenario.ParseRequestBlueprint(bytes.NewBufferString("$PRE{\n  req.skip()\n}\n\nGET http://localhost/ HTTP/1.1\r\n\r\n"), "skip.sinq")
	if err != nil {
		t.Fatalf("Failed to parse skip request: %v", err)
	}

	failBP, err := scenario.ParseRequestBlueprint(bytes.NewBufferString("GET http://localhost/ HTTP/1.1\r\n\r\n$ASSERT{\n  sinq.assert.fail(\"assertion failed\")\n}\n"), "fail.sinq")
	if err != nil {
		t.Fatalf("Failed to parse fail request: %v", err)
	}

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config: &scenario.ScenarioConfig{
				Name:          "SkipThenFailScenario",
				ReqTimeout:    scenario.Duration{Duration: 1 * time.Second},
				ScriptTimeout: scenario.Duration{Duration: 1 * time.Second},
			},
			Requests: []*scenario.RequestBlueprint{skipBP, failBP},
		},
		workspace: &mockWorkspace{},
		env:       map[string]any{"base_url": "http://localhost"},
		labels:    []string{},
	}
	
	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)
	w.resCh = resCh
	w.errorCh = errorCh

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != Failure {
			t.Errorf("Expected scenario status to be Failure, got %v: %+v", res.Status, res.RequestResults)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Worker did not return a result")
	}
}
