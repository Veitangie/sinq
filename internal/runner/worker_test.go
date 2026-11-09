// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/luapi"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
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
	w.lc.SetupStageEnvironment(0, true)

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
	w.lc.SetupStageEnvironment(0, true)

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

	ctx, cancel := context.WithCancel(context.Background())

	w := setupTestWorker(t, ctx)
	w.taskCh = taskCh

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

func TestWorker_ProcessScenario_EmptyRequestFailsCleanly(t *testing.T) {

	w := setupTestWorker(t, nil)
	resCh := observe(t, w).results

	bundle := taskBundle{
		Config: &scenario.ScenarioConfig{
			Name:       "EmptyRequestScenario",
			ReqTimeout: scenario.Duration{Duration: 1 * time.Second},
		},
		Stages:    []*scenario.StageBlueprint{{Filename: "req1.sinq"}},
		workspace: &mockWorkspace{FS: fstest.MapFS{}},
		env:       map[string]any{},
		labels:    []string{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != event.Error {
			t.Errorf("Expected scenario status to be Error for an empty request, got %v", res.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario deadlocked on an empty request")
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
	w.lc = luapi.NewLuaContext(timer.DefaultClock{}, w.env.cfg.Unrestricted, nil, nil)

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

	skipBP, err := scenario.ParseStageBlueprints(bytes.NewBufferString("$PRE{\n  req.skip()\n}\n\nGET http://localhost/ HTTP/1.1\r\n\r\n"), "skip.sinq", "")
	if err != nil {
		t.Fatalf("Failed to parse skip request: %v", err)
	}

	failBP, err := scenario.ParseStageBlueprints(bytes.NewBufferString("GET http://localhost/ HTTP/1.1\r\n\r\n$ASSERT{\n  sinq.assert.fail(\"assertion failed\")\n}\n"), "fail.sinq", "")
	if err != nil {
		t.Fatalf("Failed to parse fail request: %v", err)
	}

	bundle := taskBundle{
		Config: &scenario.ScenarioConfig{
			Name:          "SkipThenFailScenario",
			ReqTimeout:    scenario.Duration{Duration: 1 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 1 * time.Second},
		},
		Stages:    []*scenario.StageBlueprint{skipBP[0], failBP[0]},
		workspace: &mockWorkspace{},
		env:       map[string]any{"base_url": "http://localhost"},
		labels:    []string{},
		run:       true,
	}

	resCh := observe(t, w).results

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != event.Failure {
			t.Errorf("Expected scenario status to be Failure, got %v: %+v", res.Status, res.StageResults)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Worker did not return a result")
	}
}

type countingRoundTripper struct{ sent atomic.Int32 }

func (r *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.sent.Add(1)
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

func runRepeatedStageScenario(t *testing.T, items, failOn []string, setup ...func(*worker)) (aggregator.ScenarioResult, int) {
	t.Helper()
	return runScenarioSource(t, repeatedStageSource(items, failOn), setup...)
}

func repeatedStageSource(items, failOn []string) string {
	quote := func(list []string) string {
		quoted := make([]string, len(list))
		for i, item := range list {
			quoted[i] = `"` + item + `"`
		}
		return strings.Join(quoted, ", ")
	}

	raw := `### Setup
GET http://localhost/setup HTTP/1.1

$POST{ items = { ` + quote(items) + ` }; failOn = {}; for _, v in ipairs({ ` + quote(failOn) + ` }) do failOn[v] = true end }

### Loop
GET http://localhost/loop?id=${ return items[1] } HTTP/1.1

$ASSERT{ sinq.assert.isTrue(not failOn[items[1]], "iteration for " .. items[1] .. " failed") }
$POST{ table.remove(items, 1); if #items > 0 then sinq.setNextStage(#sinq.responses) end }

### After
GET http://localhost/after HTTP/1.1
`
	return raw
}

func luaSet(list []string) string {
	entries := make([]string, len(list))
	for i, item := range list {
		entries[i] = `["` + item + `"] = true`
	}
	return "{ " + strings.Join(entries, ", ") + " }"
}

func runScenarioSource(t *testing.T, raw string, setup ...func(*worker)) (aggregator.ScenarioResult, int) {
	t.Helper()
	res, sent := runScenarioSourceAllowingErrors(t, raw, setup...)
	for _, rr := range res.StageResults {
		if rr.Status == event.Error {
			t.Fatalf("Stage %q errored, the scenario itself is broken: %s", rr.Name, rr.ErrorMessage)
		}
	}
	return res, sent
}

func runScenarioSourceAllowingErrors(t *testing.T, raw string, setup ...func(*worker)) (aggregator.ScenarioResult, int) {
	t.Helper()
	w := setupTestWorker(t, nil)
	transport := &countingRoundTripper{}
	w.env.transport = transport
	for _, fn := range setup {
		fn(w)
	}

	bps, err := scenario.ParseStageBlueprints(strings.NewReader(raw), "repeat.sinq", "")
	if err != nil {
		t.Fatalf("Failed to parse requests: %v", err)
	}

	bundle := taskBundle{
		Config: &scenario.ScenarioConfig{
			Name:          "RepeatedStage",
			ReqTimeout:    scenario.Duration{Duration: 1 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 1 * time.Second},
			MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
			MaxRetries:    3,
			FailFast:      false,
		},
		Stages:    bps,
		workspace: &mockWorkspace{},
		labels:    []string{},
		run:       true,
	}

	resCh := observe(t, w).results

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if len(res.StageResults) != 3 {
			t.Fatalf("Expected 3 stage results, got %d: %+v", len(res.StageResults), res.StageResults)
		}
		return res, int(transport.sent.Load())
	case <-time.After(2 * time.Second):
		t.Fatal("Worker did not return a result")
	}
	return aggregator.ScenarioResult{}, 0
}

func TestWorker_ProcessScenario_RepeatedStage_SendsEveryPass(t *testing.T) {
	_, sent := runRepeatedStageScenario(t, []string{"a", "b", "c"}, nil)
	if sent != 5 {
		t.Errorf("Expected 5 requests on the wire (Setup, Loop x3, After), got %d", sent)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_EarlierFailedPassFailsStage(t *testing.T) {
	res, _ := runRepeatedStageScenario(t, []string{"a", "b", "c"}, []string{"a"})

	if res.Status != event.Failure {
		t.Errorf("Expected scenario status Failure, got %v", res.Status)
	}
	loop := res.StageResults[1]
	if loop.Status != event.Failure {
		t.Errorf("Expected Loop to be marked Failure because one of its passes failed, got %v", loop.Status)
	}
	if !slices.Contains(loop.FailedAssertions, "iteration for a failed") {
		t.Errorf("Expected Loop to keep the failure from its first pass, got %v", loop.FailedAssertions)
	}
	if res.StageResults[0].Status != event.Success || res.StageResults[2].Status != event.Success {
		t.Errorf("Expected Setup and After to succeed, got %v and %v", res.StageResults[0].Status, res.StageResults[2].Status)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_LastFailedPassFailsStage(t *testing.T) {
	res, _ := runRepeatedStageScenario(t, []string{"a", "b", "c"}, []string{"c"})

	loop := res.StageResults[1]
	if loop.Status != event.Failure {
		t.Errorf("Expected Loop to be marked Failure, got %v", loop.Status)
	}
	if !slices.Contains(loop.FailedAssertions, "iteration for c failed") {
		t.Errorf("Expected Loop to keep the failure from its last pass, got %v", loop.FailedAssertions)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_KeepsFailuresFromEveryPass(t *testing.T) {
	res, _ := runRepeatedStageScenario(t, []string{"a", "b", "c"}, []string{"a", "c"})

	loop := res.StageResults[1]
	if loop.Status != event.Failure {
		t.Errorf("Expected Loop to be marked Failure, got %v", loop.Status)
	}
	want := []string{"iteration for a failed", "iteration for c failed"}
	if !slices.Equal(loop.FailedAssertions, want) {
		t.Errorf("Expected Loop to keep the failures of all its failed passes in order %v, got %v", want, loop.FailedAssertions)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_AllPassesSucceed(t *testing.T) {
	res, _ := runRepeatedStageScenario(t, []string{"a", "b", "c"}, nil)

	if res.Status != event.Success {
		t.Errorf("Expected scenario status Success, got %v", res.Status)
	}
	for _, rr := range res.StageResults {
		if rr.Status != event.Success || len(rr.FailedAssertions) != 0 {
			t.Errorf("Expected stage %q to succeed with no failures, got %v %v", rr.Name, rr.Status, rr.FailedAssertions)
		}
	}
}

type loopPlan struct {
	items, failOn, skipOn, retryOn []string
}

func runPreDrivenLoopScenario(t *testing.T, plan loopPlan) (aggregator.ScenarioResult, int) {
	t.Helper()
	items := make([]string, len(plan.items))
	for i, item := range plan.items {
		items[i] = `"` + item + `"`
	}

	raw := `### Setup
GET http://localhost/setup HTTP/1.1

$POST{ items = { ` + strings.Join(items, ", ") + ` }; failOn = ` + luaSet(plan.failOn) + `; skipOn = ` + luaSet(plan.skipOn) + `; retryOn = ` + luaSet(plan.retryOn) + ` }

### Loop
GET http://localhost/loop?id=${ return current } HTTP/1.1

$PRE{ current = table.remove(items, 1); if #items > 0 then sinq.setNextStage(2) end; if skipOn[current] then req.skip() end }
$RETRY{ if retryOn[current] and res.attempt < 2 then return 0 end; return -1 }
$ASSERT{ sinq.assert.isTrue(not failOn[current], "iteration for " .. current .. " failed") }

### After
GET http://localhost/after HTTP/1.1
`
	return runScenarioSource(t, raw)
}

func TestWorker_ProcessScenario_RepeatedStage_SkippedThenRanPassIsSuccess(t *testing.T) {
	res, sent := runPreDrivenLoopScenario(t, loopPlan{items: []string{"a", "b", "c"}, skipOn: []string{"a"}})

	if sent != 4 {
		t.Fatalf("Expected 4 requests on the wire (Setup, Loop for b and c, After), got %d", sent)
	}
	if loop := res.StageResults[1]; loop.Status != event.Success {
		t.Errorf("Expected Loop to be Success since two of its passes ran and passed, got %v", loop.Status)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_RanThenSkippedPassIsSuccess(t *testing.T) {
	res, _ := runPreDrivenLoopScenario(t, loopPlan{items: []string{"a", "b", "c"}, skipOn: []string{"c"}})

	if loop := res.StageResults[1]; loop.Status != event.Success {
		t.Errorf("Expected Loop to be Success since two of its passes ran and passed, got %v", loop.Status)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_FailedThenSkippedPassIsFailure(t *testing.T) {
	res, _ := runPreDrivenLoopScenario(t, loopPlan{items: []string{"a", "b", "c"}, failOn: []string{"a"}, skipOn: []string{"b"}})

	loop := res.StageResults[1]
	if loop.Status != event.Failure {
		t.Errorf("Expected Loop to stay Failure after a later pass was skipped, got %v", loop.Status)
	}
	if !slices.Equal(loop.FailedAssertions, []string{"iteration for a failed"}) {
		t.Errorf("Expected Loop to keep the failure from its first pass, got %v", loop.FailedAssertions)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_AttemptsMatchRequestsSent(t *testing.T) {
	tests := []struct {
		name string
		plan loopPlan
		sent int
	}{
		{name: "repeats only", plan: loopPlan{items: []string{"a", "b", "c"}}, sent: 5},
		{name: "repeats with a retry", plan: loopPlan{items: []string{"a", "b", "c"}, retryOn: []string{"a"}}, sent: 6},
		{name: "repeats with a skipped pass", plan: loopPlan{items: []string{"a", "b", "c"}, skipOn: []string{"b"}}, sent: 4},
		{name: "retry and skipped pass", plan: loopPlan{items: []string{"a", "b", "c"}, retryOn: []string{"a"}, skipOn: []string{"b"}}, sent: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, sent := runPreDrivenLoopScenario(t, tt.plan)
			if sent != tt.sent {
				t.Fatalf("Expected %d requests on the wire, got %d", tt.sent, sent)
			}

			attempts := 0
			for _, rr := range res.StageResults {
				attempts += rr.Attempts
			}
			if attempts != sent {
				t.Errorf("Expected Attempts across all stages to add up to the %d requests sent, got %d: %+v", sent, attempts, res.StageResults)
			}
		})
	}
}

func TestWorker_ProcessScenario_RepeatedStage_DumpIsFromFailingPass(t *testing.T) {
	res, _ := runRepeatedStageScenario(t, []string{"a", "b", "c"}, []string{"a"}, func(w *worker) { w.env.cfg.DumpOnFailure = true })

	loop := res.StageResults[1]
	if loop.Request == "" || loop.Response == "" {
		t.Fatalf("Expected a dump for the failed Loop stage, got request %q and response %q", loop.Request, loop.Response)
	}
	if !strings.Contains(loop.Request, "id=a") {
		t.Errorf("Expected the Loop dump to show the request of its failing pass (id=a), got %q", loop.Request)
	}
}

type failingRoundTripper struct {
	countingRoundTripper
	failQuery string
	failErr   error
}

func (r *failingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, _ := r.countingRoundTripper.RoundTrip(req)
	if req.URL.RawQuery == r.failQuery {
		return nil, r.failErr
	}
	return resp, nil
}

func TestWorker_ProcessScenario_RepeatedStage_LaterPassInterruptedIsAborted(t *testing.T) {
	res, _ := runRepeatedStageScenario(t, []string{"a", "b", "c"}, nil, func(w *worker) {
		w.env.transport = &failingRoundTripper{failQuery: "id=c", failErr: context.Canceled}
	})

	if res.Status != event.Aborted {
		t.Errorf("Expected scenario status Aborted, got %v", res.Status)
	}
	if loop := res.StageResults[1]; loop.Status != event.Aborted {
		t.Errorf("Expected Loop to be Aborted since its last pass was interrupted, got %v", loop.Status)
	}
}

func TestWorker_ProcessScenario_RepeatedStage_ErrorAfterFailedPassIsError(t *testing.T) {
	res, _ := runScenarioSourceAllowingErrors(t, repeatedStageSource([]string{"a", "b", "c"}, []string{"a"}), func(w *worker) {
		w.env.transport = &failingRoundTripper{failQuery: "id=c", failErr: errors.New("connection refused")}
	})

	loop := res.StageResults[1]
	if loop.Status != event.Error {
		t.Errorf("Expected Loop to be Error since its last pass errored, got %v", loop.Status)
	}
	if !slices.Equal(loop.FailedAssertions, []string{"iteration for a failed"}) {
		t.Errorf("Expected Loop to keep the failure from its first pass, got %v", loop.FailedAssertions)
	}
	if !strings.Contains(loop.ErrorMessage, "connection refused") {
		t.Errorf("Expected Loop to carry the error of its last pass, got %q", loop.ErrorMessage)
	}
}

func cachedVia(w *worker) { w.env.cachedProcessor.transport = w.env.transport }

func TestWorker_ProcessScenario_CacheKey_RetriesSendAgain(t *testing.T) {
	tests := []struct {
		name  string
		cache string
	}{
		{name: "default key", cache: `req.cache()`},
		{name: "custom key", cache: `req.cache("poll")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `### Setup
GET http://localhost/setup HTTP/1.1

### Poll
GET http://localhost/poll HTTP/1.1

$PRE{ ` + tt.cache + ` }
$RETRY{ if res.attempt < 3 then return 0 end; return -1 }

### After
GET http://localhost/after HTTP/1.1
`
			res, sent := runScenarioSource(t, raw, cachedVia)

			if sent != 5 {
				t.Errorf("Expected each of the 3 poll attempts to reach the server (5 requests in total), got %d", sent)
			}
			if poll := res.StageResults[1]; poll.Attempts != 3 {
				t.Errorf("Expected 3 attempts for the polled stage, got %d", poll.Attempts)
			}
		})
	}
}

func TestWorker_ProcessScenario_CacheKey_SharedAcrossRequests(t *testing.T) {
	raw := `### Auth
POST http://localhost/auth HTTP/1.1

$PRE{ req.cache("login") }

### AuthToken
POST http://localhost/auth-token HTTP/1.1

$PRE{ req.cache("login") }

### Uncached
POST http://localhost/auth-token HTTP/1.1
`
	_, sent := runScenarioSource(t, raw, cachedVia)

	if sent != 2 {
		t.Errorf("Expected the two requests sharing a cache key to send once, plus the uncached one (2 in total), got %d", sent)
	}
}

func TestWorker_ProcessScenario_CacheKey_RejectsOtherArgumentTypes(t *testing.T) {
	tests := []struct {
		name    string
		call    string
		wantErr string
	}{
		{name: "number", call: `req.cache(42)`, wantErr: "Unexpected argument type number"},
		{name: "explicit nil", call: `req.cache(nil)`, wantErr: "Unexpected argument type nil"},
		{name: "missing env value", call: `req.cache(env.missing)`, wantErr: "Unexpected argument type nil"},
		{name: "table", call: `req.cache({})`, wantErr: "Unexpected argument type table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := `### Setup
GET http://localhost/setup HTTP/1.1

### Cached
GET http://localhost/cached HTTP/1.1

$PRE{ ` + tt.call + ` }

### After
GET http://localhost/after HTTP/1.1
`
			res, sent := runScenarioSourceAllowingErrors(t, raw, cachedVia)

			cached := res.StageResults[1]
			if cached.Status != event.Error || !strings.Contains(cached.ErrorMessage, tt.wantErr) {
				t.Errorf("Expected the stage to error with %q, got %v %q", tt.wantErr, cached.Status, cached.ErrorMessage)
			}
			if sent != 1 {
				t.Errorf("Expected only Setup to be sent, got %d requests", sent)
			}
		})
	}
}

func TestWorker_MaterializeRequest_UnsupportedInlineTypesFail(t *testing.T) {
	tests := []struct {
		name     string
		script   string
		typeName string
	}{
		{name: "function", script: `return function() end`, typeName: "function"},
		{name: "channel", script: `return channel.make()`, typeName: "channel"},
		{name: "coroutine", script: `return coroutine.create(function() end)`, typeName: "thread"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setupTestWorker(t, context.Background())

			source := []byte(tt.script)
			req := &scenario.StageBlueprint{
				Source:   source,
				Filename: "inline_types.sinq",
				Content: []scenario.Token{
					{Type: scenario.Script, Name: "INLINE_TYPE", Line: 1, PayloadStart: 0, PayloadEnd: len(source)},
				},
			}

			materialized, err := w.materializeRequest(context.Background(), req, time.Second)
			if err == nil {
				t.Fatalf("expected an inline %s to fail materialization, got %q", tt.name, materialized)
			}
			if !strings.Contains(err.Error(), "unexpected type "+tt.typeName) {
				t.Errorf("expected the error to name the %s type, got %v", tt.typeName, err)
			}
		})
	}
}

func TestWorker_MaterializeRequest_UnserializableInlineTableFails(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	rawSinq := `POST http://localhost/ HTTP/1.1

{ "ids": ${ return { 1, nil, 3 } } }
`
	reqBp, err := scenario.ParseStageBlueprints(strings.NewReader(rawSinq), "sparse_inline.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	materialized, err := w.materializeRequest(context.Background(), reqBp[0], time.Second)
	if err == nil {
		t.Fatalf("expected materialization to fail on a sparse inline table, got %q", materialized)
	}
	if !strings.Contains(err.Error(), "sparse_inline.sinq#3:") || !strings.Contains(err.Error(), "Failed to materialize request") || !strings.Contains(err.Error(), "sparse array") {
		t.Errorf("expected a located materialization error naming the sparse array, got %v", err)
	}
}

func TestWorker_ProcessScenario_CacheKey_FalseDisablesEarlierCache(t *testing.T) {
	raw := `### First
GET http://localhost/same HTTP/1.1

$PRE{ req.cache("shared"); req.cache(false) }

### Second
GET http://localhost/same HTTP/1.1

$PRE{ req.cache("shared"); req.cache(false) }

### After
GET http://localhost/after HTTP/1.1
`
	_, sent := runScenarioSource(t, raw, cachedVia)

	if sent != 3 {
		t.Errorf("Expected req.cache(false) to override the earlier key so every request is sent (3), got %d", sent)
	}
}

func TestWorker_ProcessScenario_CacheKey_NoArgumentEnablesDefaultCache(t *testing.T) {
	raw := `### First
GET http://localhost/same HTTP/1.1

$PRE{ req.cache() }

### Second
GET http://localhost/same HTTP/1.1

$PRE{ req.cache() }

### After
GET http://localhost/after HTTP/1.1
`
	_, sent := runScenarioSource(t, raw, cachedVia)

	if sent != 2 {
		t.Errorf("Expected the identical cached requests to send once, plus After (2 in total), got %d", sent)
	}
}

func TestWorker_ScenarioEnd_ExtractsOutput(t *testing.T) {
	ctx := context.Background()
	w := setupTestWorker(t, ctx)
	obs := observe(t, w)

	buf := bytes.NewBufferString("hello world\n")
	w.lc = luapi.NewLuaContext(timer.DefaultClock{}, false, nil, buf)

	w.send(w.scenarioEnd(ctx, timer.NewTimer(timer.DefaultClock{})))

	ends := obs.ofKind(event.ScenarioEnd)
	if len(ends) != 1 || ends[0].Output == nil {
		t.Fatalf("Expected one ScenarioEnd carrying the Lua output, got %+v", ends)
	}
	if ends[0].Output.String() != "hello world\n" {
		t.Errorf("Expected Output to contain 'hello world\\n', got %q", ends[0].Output.String())
	}
}

func TestWorker_Run_NilContext(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	obs := observe(t, w)

	w.run(nil) //nolint:staticcheck

	errs := obs.ofKind(event.WorkerError)
	if len(errs) != 1 || errs[0].ErrorString == "" {
		t.Fatalf("Expected run(nil) to report one WorkerError, got %+v", errs)
	}
}

func TestWorker_ProcessStage_ContextAlreadyCanceled(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	obs := observe(t, w)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{Name: "cancelled"},
		Stages: []*scenario.StageBlueprint{{Filename: "req.sinq"}},
	}

	if w.processStage(ctx, scenarioBp, 0, &http.Client{}, time.Second) {
		t.Error("expected shouldContinue to be false when the context is already cancelled")
	}
	if ends := obs.ofKind(event.StageEnd); len(ends) != 1 || ends[0].Status != event.Aborted {
		t.Errorf("expected one Aborted StageEnd for a cancelled context, got %+v", ends)
	}
}

func TestWorker_ProcessStage_ContextDeadlineExceeded(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	obs := observe(t, w)

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	time.Sleep(time.Millisecond)

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{Name: "timed-out"},
		Stages: []*scenario.StageBlueprint{{Filename: "req.sinq"}},
	}

	if w.processStage(ctx, scenarioBp, 0, &http.Client{}, time.Second) {
		t.Error("expected shouldContinue to be false when the context deadline is exceeded")
	}
	if ends := obs.ofKind(event.StageEnd); len(ends) != 1 || ends[0].Status != event.Error {
		t.Errorf("expected one Error StageEnd for a deadline-exceeded context, got %+v", ends)
	}
}

func TestWorker_ProcessScenario_TagFilterExcludesScenario(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	w.env.cfg.TagsInclude = []string{"nonexistent-tag"}

	resCh := observe(t, w).results

	bundle := taskBundle{
		Config: &scenario.ScenarioConfig{
			Name: "FilteredScenario",
			Tags: map[string]struct{}{"other": {}},
		},
		Stages: []*scenario.StageBlueprint{{Filename: "req1.sinq"}},
		env:    map[string]any{},
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != event.Unset && res.Status != event.Skipped {
			t.Errorf("expected an excluded scenario to report Unset status, got %v", res.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario did not report a result for a tag-filtered scenario")
	}
}

func TestWorker_ProcessScenario_RequestNamePrefersExplicitName(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	w.env.transport = mockRoundTripper{}

	resCh := observe(t, w).results

	bp, err := scenario.ParseStageBlueprints(bytes.NewBufferString("GET http://localhost/ HTTP/1.1\r\n\r\n"), "req1.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse request blueprint: %v", err)
	}
	bp[0].Name = "Custom Request Name"

	bundle := taskBundle{
		Config: &scenario.ScenarioConfig{Name: "NamedRequestScenario"},
		Stages: bp,
		env:    map[string]any{},
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if len(res.StageResults) != 1 || res.StageResults[0].Name != "Custom Request Name" {
			t.Errorf("expected the explicit request Name to be used over Filename, got %+v", res.StageResults)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario did not report a result")
	}
}

func TestWorker_MaterializeRequest_ContextCancelledMidLoop(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &scenario.StageBlueprint{
		Source: []byte("GET /\r\n\r\n"),
		Content: []scenario.Token{
			{Type: scenario.Text, Start: 0, End: 9, PayloadStart: 0, PayloadEnd: 9},
		},
	}

	_, err := w.materializeRequest(ctx, req, time.Second)
	if err == nil {
		t.Fatal("expected an error when the context is already cancelled")
	}
}

func TestWorker_MaterializeRequest_IncompleteToken(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	req := &scenario.StageBlueprint{
		Source:   []byte("GET / $BAD"),
		Filename: "bad.sinq",
		Content: []scenario.Token{
			{Type: scenario.IncompleteToken, Line: 1, Offset: 5},
		},
	}

	_, err := w.materializeRequest(context.Background(), req, time.Second)
	if err == nil {
		t.Fatal("expected an error for an incomplete token")
	}
	if !strings.Contains(err.Error(), "incomplete token") {
		t.Errorf("expected the error to mention an incomplete token, got %q", err.Error())
	}
}

func TestWorker_MaterializeRequest_UnexpectedDelimiter(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	req := &scenario.StageBlueprint{
		Source:   []byte("GET / ###"),
		Filename: "bad.sinq",
		Content: []scenario.Token{
			{Type: scenario.Delimiter, Line: 1, Offset: 6},
		},
	}

	_, err := w.materializeRequest(context.Background(), req, time.Second)
	if err == nil {
		t.Fatal("expected an error for an unexpected delimiter")
	}
	if !strings.Contains(err.Error(), "delimiter") {
		t.Errorf("expected the error to mention a delimiter, got %q", err.Error())
	}
}

func TestWorker_LoggingContext(t *testing.T) {
	w := setupTestWorker(t, nil)

	has := func(attrs []slog.Attr, key, value string) bool {
		for _, a := range attrs {
			if a.Key == key && (value == "" || a.Value.String() == value) {
				return true
			}
		}
		return false
	}

	baseCtx := context.Background()
	got := w.loggingContext(baseCtx)
	if has(got, string(scenarioNameContext), "") || has(got, string(stageNameContext), "") {
		t.Errorf("expected no scenario/request name keys for a bare context, got %v", got)
	}

	withScenario := context.WithValue(baseCtx, scenarioNameContext, "myScenario")
	got2 := w.loggingContext(withScenario)
	if !has(got2, string(scenarioNameContext), "myScenario") {
		t.Errorf("expected scenarioName to be included when present in the context, got %v", got2)
	}

	withRequest := context.WithValue(baseCtx, stageNameContext, "myRequest")
	got3 := w.loggingContext(withRequest)
	found := has(got3, string(stageNameContext), "myRequest")
	if !found {
		t.Errorf("expected requestName to be included when present in the context, got %v", got3)
	}
}

func TestWorker_ProcessStage_ClearsDumpWhenNoAssertionFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, context.Background())
	w.env.cfg.DumpOnFailure = true
	obs := observe(t, w)

	reqBp, err := scenario.ParseStageBlueprints(strings.NewReader("GET "+srv.URL+" HTTP/1.1\r\n\r\n"), "ok.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse blueprint: %v", err)
	}

	w.processScenario(context.Background(), taskBundle{
		Config: &scenario.ScenarioConfig{
			Name:          "NoFailureScenario",
			ReqTimeout:    scenario.Duration{Duration: 2 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 2 * time.Second},
			MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
		},
		Stages:    reqBp,
		workspace: &mockWorkspace{},
		run:       true,
	})

	rr := obs.result().StageResults[0]
	if rr.Request != "" || rr.Response != "" {
		t.Errorf("expected Request/Response dumps to be cleared when there are no assertion failures, got Request=%q Response=%q", rr.Request, rr.Response)
	}
	if rr.Status == event.Failure {
		t.Errorf("expected no failure when assertionFailures is empty, got %v", rr.Status)
	}
}

func TestWorker_ProcessScenario_MaxRedirectsBoundary(t *testing.T) {
	finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer finalSrv.Close()

	redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalSrv.URL, http.StatusFound)
	}))
	defer redirectSrv.Close()

	run := func(maxRedirects int) event.ResultStatus {
		w := setupTestWorker(t, context.Background())
		resCh := observe(t, w).results

		bp, err := scenario.ParseStageBlueprints(strings.NewReader("GET "+redirectSrv.URL+" HTTP/1.1\r\n\r\n"), "redirect.sinq", "")
		if err != nil {
			t.Fatalf("failed to parse blueprint: %v", err)
		}

		bundle := taskBundle{
			Config: &scenario.ScenarioConfig{
				Name:         "RedirectScenario",
				ReqTimeout:   scenario.Duration{Duration: 2 * time.Second},
				MaxRedirects: maxRedirects,
			},
			Stages:    bp,
			workspace: &mockWorkspace{},
			env:       map[string]any{},
			run:       true,
		}

		w.processScenario(context.Background(), bundle)

		select {
		case res := <-resCh:
			return res.Status
		case <-time.After(2 * time.Second):
			t.Fatal("processScenario deadlocked")
		}
		return event.Unset
	}

	if status := run(0); status != event.Error {
		t.Errorf("expected MaxRedirects=0 with exactly one redirect present to fail with Error, got %v", status)
	}
	if status := run(1); status != event.Success {
		t.Errorf("expected MaxRedirects=1 with exactly one redirect present to succeed, got %v", status)
	}
}

func TestWorker_ProcessStage_PostScriptFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, context.Background())
	w.lc.SetupStageEnvironment(0, true)

	rawSinq := "GET " + srv.URL + "\n$POST{\nerror(\"boom\")\n}"
	reqBp, err := scenario.ParseStageBlueprints(strings.NewReader(rawSinq), "post_fail.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse blueprint: %v", err)
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{
			Name:          "PostFailScenario",
			ReqTimeout:    scenario.Duration{Duration: 2 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 2 * time.Second},
		},
		Stages: reqBp,
	}

	obs := observe(t, w)
	if w.processStage(context.Background(), scenarioBp, 0, srv.Client(), scenarioBp.Config.ReqTimeout.Duration) {
		t.Error("expected shouldContinue to be false when the post script fails")
	}
	ends := obs.ofKind(event.StageEnd)
	if len(ends) == 0 || ends[len(ends)-1].Status != event.Error || ends[len(ends)-1].ErrorString == "" {
		t.Fatalf("expected the request to end with an Error from the failing post script, got %+v", ends)
	}
}

func TestWorker_ProcessScenario_SetsMaxRequestIdx(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	w.env.transport = mockRoundTripper{}
	resCh := observe(t, w).results

	bp1, _ := scenario.ParseStageBlueprints(strings.NewReader("GET http://localhost/ HTTP/1.1\r\n\r\n"), "req.sinq", "")
	bp2, _ := scenario.ParseStageBlueprints(strings.NewReader("GET http://localhost/ HTTP/1.1\r\n\r\n"), "req2.sinq", "")

	bundle := taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "TwoRequests"},
		Stages:    []*scenario.StageBlueprint{bp1[0], bp2[0]},
		workspace: &mockWorkspace{},
		env:       map[string]any{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	if w.maxStageIdx != 1 {
		t.Errorf("expected maxStageIdx to be len(Requests)-1 = 1, got %d", w.maxStageIdx)
	}

	select {
	case <-resCh:
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario did not report a result")
	}
}

func TestWorker_ProcessScenario_ZeroStages(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	resCh := observe(t, w).results

	bundle := taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "ZeroStageScenario"},
		Stages:    []*scenario.StageBlueprint{},
		workspace: &mockWorkspace{},
		env:       map[string]any{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != event.Success {
			t.Errorf("expected a scenario with zero requests to succeed trivially, got %v", res.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario deadlocked or panicked on zero requests")
	}
}

func TestWorker_ProcessScenario_LogsStageFailure(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	obs := observe(t, w)
	resCh := obs.results

	bundle := taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "FailLogScenario", ReqTimeout: scenario.Duration{Duration: time.Second}},
		Stages:    []*scenario.StageBlueprint{{Filename: "req1.sinq"}},
		workspace: &mockWorkspace{FS: fstest.MapFS{}},
		env:       map[string]any{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	select {
	case <-resCh:
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario deadlocked")
	}

	if !obs.logged("Stage failed") {
		t.Errorf("expected a 'Stage failed' debug log line when a stage errors, got logs: %+v", obs.ofKind(event.Log))
	}
}

func TestWorker_MaterializeRequest_CapacityHintDoesNotPanic(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	req := &scenario.StageBlueprint{
		Source: []byte("GET / HTTP/1.1\r\n\r\n"),
		Content: []scenario.Token{
			{Type: scenario.Text, Start: 0, End: 18, PayloadStart: 0, PayloadEnd: 18},
		},
	}

	materialized, err := w.materializeRequest(context.Background(), req, time.Second)
	if err != nil {
		t.Fatalf("unexpected error (or panic averted): %v", err)
	}
	if string(materialized) != "GET / HTTP/1.1\r\n\r\n" {
		t.Errorf("unexpected materialized content: %q", materialized)
	}
}

func TestWorker_MaterializeRequest_ScriptTokenError(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	source := []byte("if then")
	req := &scenario.StageBlueprint{
		Source:   source,
		Filename: "script_fail.sinq",
		Content: []scenario.Token{
			{Type: scenario.Script, Name: "BAD_SCRIPT", Line: 1, PayloadStart: 0, PayloadEnd: len(source)},
		},
	}

	materialized, err := w.materializeRequest(context.Background(), req, time.Second)
	if err == nil {
		t.Fatal("expected an error when the embedded script fails to execute")
	}
	if len(materialized) != 0 {
		t.Errorf("expected empty materialized bytes on script error, got %q", materialized)
	}
}

func TestWorker_MaterializeRequest_ScriptTokenSuccess(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	source := []byte(`return "computed"`)
	req := &scenario.StageBlueprint{
		Source:   source,
		Filename: "script_ok.sinq",
		Content: []scenario.Token{
			{Type: scenario.Script, Name: "OK_SCRIPT", Line: 1, PayloadStart: 0, PayloadEnd: len(source)},
		},
	}

	materialized, err := w.materializeRequest(context.Background(), req, time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(materialized) != "computed" {
		t.Errorf("expected materialized content 'computed', got %q", materialized)
	}
}

func TestWorker_MaterializeRequest_MultipleReturnValues(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	if err := w.lc.DoString(`flat = { name = "x" }`); err != nil {
		t.Fatalf("failed to seed Lua state: %v", err)
	}

	rawSinq := `POST http://localhost/ HTTP/1.1

{ "two": ${ return "first", nil }, "twoStr": "${ return "first", "second" }", "plain": ${ return sinq.json.serialize(flat) } }
`
	reqBp, err := scenario.ParseStageBlueprints(strings.NewReader(rawSinq), "multi_return.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	materialized, err := w.materializeRequest(context.Background(), reqBp[0], time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := `{ "two": first, "twoStr": "first", "plain": {"name":"x"} }`
	if !strings.Contains(string(materialized), want) {
		t.Errorf("expected materialized body to contain %q, got %q", want, materialized)
	}
}

func TestWorker_MaterializeRequest_NilScriptValueFails(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	rawSinq := `GET http://localhost/ HTTP/1.1
X-Value: ${ return nil }
`
	reqBp, err := scenario.ParseStageBlueprints(strings.NewReader(rawSinq), "nil_return.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	materialized, err := w.materializeRequest(context.Background(), reqBp[0], time.Second)
	if err == nil {
		t.Fatalf("expected materialization to fail on a nil inline value, got %q", materialized)
	}
}

func TestWorker_MaterializeRequest_InlineValueTypes(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{name: "string is inserted raw", script: `return "plain"`, want: `plain`},
		{name: "number is inserted as text", script: `return 42`, want: `42`},
		{name: "true is inserted as text", script: `return true`, want: `true`},
		{name: "false is inserted as text", script: `return false`, want: `false`},
		{name: "table is serialized as JSON", script: `return { name = "x" }`, want: `{"name":"x"}`},
		{name: "json.null is inserted as null", script: `return sinq.json.null`, want: `null`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setupTestWorker(t, context.Background())

			source := []byte(tt.script)
			req := &scenario.StageBlueprint{
				Source:   source,
				Filename: "inline_types.sinq",
				Content: []scenario.Token{
					{Type: scenario.Script, Name: "INLINE_TYPE", Line: 1, PayloadStart: 0, PayloadEnd: len(source)},
				},
			}

			materialized, err := w.materializeRequest(context.Background(), req, time.Second)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(materialized) != tt.want {
				t.Errorf("expected %q, got %q", tt.want, materialized)
			}
		})
	}
}

func TestWorker_MaterializeRequest_InlineTableIgnoresEarlierIndent(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	rawSinq := `POST http://localhost/ HTTP/1.1
X-Pretty: ${ return (sinq.json.serialize({ a = 1 }, "  ")) }
X-Table: ${ return { b = 1 } }
`
	reqBp, err := scenario.ParseStageBlueprints(strings.NewReader(rawSinq), "stale_indent.sinq", "")
	if err != nil {
		t.Fatalf("failed to parse request: %v", err)
	}

	materialized, err := w.materializeRequest(context.Background(), reqBp[0], time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "X-Table: {\"b\":1}\n"
	if !strings.Contains(string(materialized), want) {
		t.Errorf("expected the inline table to be serialized compactly as %q, got %q", want, materialized)
	}
}

func TestWorker_Close_NilLuaContext(t *testing.T) {
	w := &worker{}
	w.Close()
}
