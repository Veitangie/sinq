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
	"strings"
	"testing"
	"testing/fstest"
	"time"

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

func TestWorker_ProcessScenario_EmptyRequestFailsCleanly(t *testing.T) {
	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)

	w := setupTestWorker(t, nil)
	w.resCh = resCh
	w.errorCh = errorCh

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config: &scenario.ScenarioConfig{
				Name:       "EmptyRequestScenario",
				ReqTimeout: scenario.Duration{Duration: 1 * time.Second},
			},
			Requests: []*scenario.RequestBlueprint{{Filename: "req1.sinq"}},
		},
		workspace: &mockWorkspace{FS: fstest.MapFS{}},
		env:       map[string]any{},
		labels:    []string{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != Error {
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

	skipBP, err := scenario.ParseRequestBlueprints(bytes.NewBufferString("$PRE{\n  req.skip()\n}\n\nGET http://localhost/ HTTP/1.1\r\n\r\n"), "skip.sinq")
	if err != nil {
		t.Fatalf("Failed to parse skip request: %v", err)
	}

	failBP, err := scenario.ParseRequestBlueprints(bytes.NewBufferString("GET http://localhost/ HTTP/1.1\r\n\r\n$ASSERT{\n  sinq.assert.fail(\"assertion failed\")\n}\n"), "fail.sinq")
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
			Requests: []*scenario.RequestBlueprint{skipBP[0], failBP[0]},
		},
		workspace: &mockWorkspace{},
		env:       map[string]any{"base_url": "http://localhost"},
		labels:    []string{},
		run:       true,
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

func TestWorker_ReportResult_ExtractsOutput(t *testing.T) {
	ctx := context.Background()
	w := setupTestWorker(t, ctx)

	resCh := make(chan ScenarioResult, 1)
	w.resCh = resCh

	buf := bytes.NewBufferString("hello world\n")
	w.lc = luapi.NewLuaContext(timer.DefaultClock{}, false, nil, buf)

	scenarioTimer := timer.NewTimer(timer.DefaultClock{})
	result := ScenarioResult{Name: "test_scenario"}

	go w.reportResult(ctx, scenarioTimer, result)

	select {
	case res := <-resCh:
		if res.Output == nil {
			t.Fatal("Expected Output to be populated in ScenarioResult, got nil")
		}
		if res.Output.String() != "hello world\n" {
			t.Errorf("Expected Output to contain 'hello world\\n', got %q", res.Output.String())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("reportResult deadlocked")
	}
}

func TestWorker_Run_NilContext(t *testing.T) {
	errorCh := make(chan error, 1)
	w := setupTestWorker(t, context.Background())
	w.errorCh = errorCh

	w.run(nil) //nolint:staticcheck

	select {
	case err := <-errorCh:
		if err == nil {
			t.Fatal("expected an error for a nil context, got nil")
		}
	default:
		t.Fatal("expected run(nil) to report an error on errorCh")
	}
}

func TestWorker_ProcessRequest_ContextAlreadyCanceled(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	scenarioBp := &scenario.ScenarioBlueprint{
		Config:   &scenario.ScenarioConfig{Name: "cancelled"},
		Requests: []*scenario.RequestBlueprint{{Filename: "req.sinq"}},
	}
	status := Success
	result := &RequestResult{}

	shouldContinue, err := w.processRequest(ctx, scenarioBp, 0, &http.Client{}, &status, result)
	if shouldContinue {
		t.Error("expected shouldContinue to be false when the context is already cancelled")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if status != Aborted {
		t.Errorf("expected status Aborted for a cancelled context, got %v", status)
	}
	if result.Status != Aborted {
		t.Errorf("expected result.Status Aborted, got %v", result.Status)
	}
}

func TestWorker_ProcessRequest_ContextDeadlineExceeded(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	time.Sleep(time.Millisecond)

	scenarioBp := &scenario.ScenarioBlueprint{
		Config:   &scenario.ScenarioConfig{Name: "timed-out"},
		Requests: []*scenario.RequestBlueprint{{Filename: "req.sinq"}},
	}
	status := Success
	result := &RequestResult{}

	shouldContinue, err := w.processRequest(ctx, scenarioBp, 0, &http.Client{}, &status, result)
	if shouldContinue {
		t.Error("expected shouldContinue to be false when the context deadline is exceeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if status != Error {
		t.Errorf("expected status Error for a deadline-exceeded context, got %v", status)
	}
	if result.Status != Error {
		t.Errorf("expected result.Status Error, got %v", result.Status)
	}
}

func TestWorker_ProcessScenario_TagFilterExcludesScenario(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	w.env.cfg.TagsInclude = []string{"nonexistent-tag"}

	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)
	w.resCh = resCh
	w.errorCh = errorCh

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config: &scenario.ScenarioConfig{
				Name: "FilteredScenario",
				Tags: map[string]struct{}{"other": {}},
			},
			Requests: []*scenario.RequestBlueprint{{Filename: "req1.sinq"}},
		},
		env: map[string]any{},
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != Unset {
			t.Errorf("expected an excluded scenario to report Unset status, got %v", res.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario did not report a result for a tag-filtered scenario")
	}
}

func TestWorker_ProcessScenario_RequestNamePrefersExplicitName(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	w.env.transport = mockRoundTripper{}

	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)
	w.resCh = resCh
	w.errorCh = errorCh

	bp, err := scenario.ParseRequestBlueprints(bytes.NewBufferString("GET http://localhost/ HTTP/1.1\r\n\r\n"), "req1.sinq")
	if err != nil {
		t.Fatalf("failed to parse request blueprint: %v", err)
	}
	bp[0].Name = "Custom Request Name"

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config:   &scenario.ScenarioConfig{Name: "NamedRequestScenario"},
			Requests: bp,
		},
		env: map[string]any{},
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if len(res.RequestResults) != 1 || res.RequestResults[0].Name != "Custom Request Name" {
			t.Errorf("expected the explicit request Name to be used over Filename, got %+v", res.RequestResults)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario did not report a result")
	}
}

func TestWorker_MaterializeRequest_ContextCancelledMidLoop(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &scenario.RequestBlueprint{
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

	req := &scenario.RequestBlueprint{
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

	req := &scenario.RequestBlueprint{
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

	baseCtx := context.Background()
	got := w.loggingContext(baseCtx)
	for i := 0; i+1 < len(got); i += 2 {
		if got[i] == string(scenarioNameContext) || got[i] == string(requestNameContext) {
			t.Errorf("expected no scenario/request name keys for a bare context, got %v", got)
		}
	}

	withScenario := context.WithValue(baseCtx, scenarioNameContext, "myScenario")
	got2 := w.loggingContext(withScenario)
	found := false
	for i := 0; i+1 < len(got2); i += 2 {
		if got2[i] == string(scenarioNameContext) && got2[i+1] == "myScenario" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scenarioName to be included when present in the context, got %v", got2)
	}

	withRequest := context.WithValue(baseCtx, requestNameContext, "myRequest")
	got3 := w.loggingContext(withRequest)
	found = false
	for i := 0; i+1 < len(got3); i += 2 {
		if got3[i] == string(requestNameContext) && got3[i+1] == "myRequest" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected requestName to be included when present in the context, got %v", got3)
	}
}

func TestWorker_ProcessRequest_ClearsDumpWhenNoAssertionFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, context.Background())
	w.lc.SetupRequestEnvironment(0)
	w.env.cfg.DumpOnFailure = true

	reqBp, err := scenario.ParseRequestBlueprints(strings.NewReader("GET "+srv.URL+" HTTP/1.1\r\n\r\n"), "ok.sinq")
	if err != nil {
		t.Fatalf("failed to parse blueprint: %v", err)
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{
			Name:       "NoFailureScenario",
			ReqTimeout: scenario.Duration{Duration: 2 * time.Second},
		},
		Requests: reqBp,
	}

	status := Success
	result := &RequestResult{}

	if _, err := w.processRequest(context.Background(), scenarioBp, 0, srv.Client(), &status, result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Request != "" || result.Response != "" {
		t.Errorf("expected Request/Response dumps to be cleared when there are no assertion failures, got Request=%q Response=%q", result.Request, result.Response)
	}
	if status == Failure || result.Status == Failure {
		t.Errorf("expected no failure when assertionFailures is empty, got status=%v result.Status=%v", status, result.Status)
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

	run := func(maxRedirects int) ResultStatus {
		w := setupTestWorker(t, context.Background())
		resCh := make(chan ScenarioResult, 1)
		errorCh := make(chan error, 1)
		w.resCh = resCh
		w.errorCh = errorCh

		bp, err := scenario.ParseRequestBlueprints(strings.NewReader("GET "+redirectSrv.URL+" HTTP/1.1\r\n\r\n"), "redirect.sinq")
		if err != nil {
			t.Fatalf("failed to parse blueprint: %v", err)
		}

		bundle := taskBundle{
			ScenarioBlueprint: scenario.ScenarioBlueprint{
				Config: &scenario.ScenarioConfig{
					Name:         "RedirectScenario",
					ReqTimeout:   scenario.Duration{Duration: 2 * time.Second},
					MaxRedirects: maxRedirects,
				},
				Requests: bp,
			},
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
		return Unset
	}

	if status := run(1); status != Error {
		t.Errorf("expected MaxRedirects=1 with exactly one redirect present to fail with Error, got %v", status)
	}
	if status := run(2); status != Success {
		t.Errorf("expected MaxRedirects=2 with exactly one redirect present to succeed, got %v", status)
	}
}

func TestWorker_ProcessRequest_PostScriptFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, context.Background())
	w.lc.SetupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n$POST{\nerror(\"boom\")\n}"
	reqBp, err := scenario.ParseRequestBlueprints(strings.NewReader(rawSinq), "post_fail.sinq")
	if err != nil {
		t.Fatalf("failed to parse blueprint: %v", err)
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{
			Name:          "PostFailScenario",
			ReqTimeout:    scenario.Duration{Duration: 2 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 2 * time.Second},
		},
		Requests: reqBp,
	}

	status := Success
	result := &RequestResult{}

	shouldContinue, err := w.processRequest(context.Background(), scenarioBp, 0, srv.Client(), &status, result)
	if shouldContinue {
		t.Error("expected shouldContinue to be false when the post script fails")
	}
	if err == nil {
		t.Fatal("expected an error from the failing post script")
	}
}

func TestWorker_ProcessScenario_SetsMaxRequestIdx(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	w.env.transport = mockRoundTripper{}
	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)
	w.resCh = resCh
	w.errorCh = errorCh

	bp1, _ := scenario.ParseRequestBlueprints(strings.NewReader("GET http://localhost/ HTTP/1.1\r\n\r\n"), "req.sinq")
	bp2, _ := scenario.ParseRequestBlueprints(strings.NewReader("GET http://localhost/ HTTP/1.1\r\n\r\n"), "req2.sinq")

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config:   &scenario.ScenarioConfig{Name: "TwoRequests"},
			Requests: []*scenario.RequestBlueprint{bp1[0], bp2[0]},
		},
		workspace: &mockWorkspace{},
		env:       map[string]any{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	if w.maxRequestIdx != 1 {
		t.Errorf("expected maxRequestIdx to be len(Requests)-1 = 1, got %d", w.maxRequestIdx)
	}

	select {
	case <-resCh:
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario did not report a result")
	}
}

func TestWorker_ProcessScenario_ZeroRequests(t *testing.T) {
	w := setupTestWorker(t, context.Background())
	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)
	w.resCh = resCh
	w.errorCh = errorCh

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config:   &scenario.ScenarioConfig{Name: "ZeroRequestScenario"},
			Requests: []*scenario.RequestBlueprint{},
		},
		workspace: &mockWorkspace{},
		env:       map[string]any{},
		run:       true,
	}

	w.processScenario(context.Background(), bundle)

	select {
	case res := <-resCh:
		if res.Status != Success {
			t.Errorf("expected a scenario with zero requests to succeed trivially, got %v", res.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("processScenario deadlocked or panicked on zero requests")
	}
}

func TestWorker_ProcessScenario_LogsRequestFailure(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	w := setupTestWorker(t, context.Background())
	w.env.logger = logger

	resCh := make(chan ScenarioResult, 1)
	errorCh := make(chan error, 1)
	w.resCh = resCh
	w.errorCh = errorCh

	bundle := taskBundle{
		ScenarioBlueprint: scenario.ScenarioBlueprint{
			Config:   &scenario.ScenarioConfig{Name: "FailLogScenario", ReqTimeout: scenario.Duration{Duration: time.Second}},
			Requests: []*scenario.RequestBlueprint{{Filename: "req1.sinq"}},
		},
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

	if !strings.Contains(buf.String(), "Request failed") {
		t.Errorf("expected a 'Request failed' debug log line when processRequest returns an error, got log: %q", buf.String())
	}
}

func TestWorker_MaterializeRequest_CapacityHintDoesNotPanic(t *testing.T) {
	w := setupTestWorker(t, context.Background())

	req := &scenario.RequestBlueprint{
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
	req := &scenario.RequestBlueprint{
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
	req := &scenario.RequestBlueprint{
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

func TestWorker_Close_NilLuaContext(t *testing.T) {
	w := &worker{}
	w.Close()
}
