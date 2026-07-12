// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
)

func TestRequestProcessor_ContextCancellationDuringRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	w := setupTestWorker(t, ctx)
	w.lc.setupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n$RETRY{\n return 10000 \n}"
	reqBp, err := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "retry_test.sinq")
	if err != nil {
		t.Fatalf("Failed to parse blueprint: %v", err)
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{
			MaxRetries:    3,
			ReqTimeout:    scenario.Duration{Duration: 30 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 30 * time.Second},
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

func TestBug_SaveToLeak(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	dummyExtract := func(scenario.Token) []byte { return []byte("") }

	_, _, err := w.runPreScript(scenario.Token{}, dummyExtract, "test.sinq", 1*time.Second)
	if err != nil {
		t.Fatalf("runPreScript failed: %v", err)
	}

	saveToVal := w.lc.requestTable.RawGetString("saveResponseTo")
	if saveToVal.Type().String() != "nil" {
		t.Errorf("BUG EXPOSED: 'saveResponseTo' leaked! Expected LNil in requestTable, got %s", saveToVal.Type().String())
	}
}

func TestBug_ZeroByteUpload(t *testing.T) {
	var receivedBody []byte
	var receivedTransferEncoding []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTransferEncoding = r.TransferEncoding
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	payload := []byte("hello chunked world")
	mockFS := fstest.MapFS{
		"upload.txt": &fstest.MapFile{Data: payload},
	}

	w := setupTestWorker(t, nil)
	w.env.workspace = &mockWorkspace{FS: mockFS}
	w.lc.setupRequestEnvironment(0)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		httpRequest:  req,
		client:       server.Client(),
		filenameFrom: "upload.txt",
		requestTimer: newTimer(DefaultClock{}),
		result:       &RequestResult{},
		status:       new(ResultStatus),
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
	}

	err := processor.send()
	if err != nil {
		t.Fatalf("send() failed: %v", err)
	}

	if len(receivedBody) == 0 {
		t.Errorf("BUG EXPOSED: Server received 0 bytes! ContentLength was 0, so the attached file was ignored.")
	} else if string(receivedBody) != string(payload) {
		t.Errorf("Expected body %q, got %q", string(payload), string(receivedBody))
	}

	isChunked := false
	for _, te := range receivedTransferEncoding {
		if te == "chunked" {
			isChunked = true
		}
	}
	if !isChunked {
		t.Errorf("Expected Transfer-Encoding: chunked, but it wasn't set.")
	}
}

func TestWorker_AttachInvalidFileFastFails(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	script := []byte(`req.attach("missing.txt")`)
	extract := func(scenario.Token) []byte { return script }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	_, _, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second)

	if err == nil {
		t.Fatal("Expected runPreScript to fail on missing file, but it succeeded")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("Expected invalid file path error, got: %v", err)
	}
}

func TestRequestProcessor_FailsIfBodyAndFileAttached(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	rawRequest := "POST http://localhost\n\nthis is a body"

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		materialized: []byte(rawRequest),
		filenameFrom: "attached_file.txt",
		requestTimer: newTimer(DefaultClock{}),
		result:       &RequestResult{},
		status:       new(ResultStatus),
	}

	err := processor.parse()

	if err == nil {
		t.Fatal("Expected parse to fail because both body and file source exist")
	}
	if !strings.Contains(err.Error(), "both body and a file source") {
		t.Errorf("Expected conflict error, got: %v", err)
	}
}

func TestRequestProcessor_MaxRetriesExceeded(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n$RETRY{\n return 1 \n}"
	reqBp, err := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "retry_test.sinq")
	if err != nil {
		t.Fatalf("Failed to parse blueprint: %v", err)
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{
			MaxRetries:    2,
			ReqTimeout:    scenario.Duration{Duration: 5 * time.Second},
			ScriptTimeout: scenario.Duration{Duration: 5 * time.Second},
		},
	}

	status := Success
	result := &RequestResult{}

	processor := RequestProcessor{
		w:            w,
		ctx:          context.Background(),
		scenarioBp:   scenarioBp,
		requestBp:    reqBp,
		status:       &status,
		result:       result,
		requestTimer: newTimer(DefaultClock{}),
		client:       srv.Client(),
	}

	_ = processor.materialize()
	_ = processor.parse()

	err = processor.run()

	if err == nil || err.Error() != "Too many retries" {
		t.Fatalf("Expected 'Too many retries' error, got: %v", err)
	}
	if attempts != 3 {
		t.Errorf("Expected exactly 3 server hits, got %d", attempts)
	}
}

func TestRequestProcessor_BadTLS_FailsGracefully(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "/path HTTP/1.1\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "tls_test.sinq")

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		client:       &http.Client{}, // Deliberately bypass test TLS verification
		requestBp:    reqBp,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
		status:       new(ResultStatus),
		result:       &RequestResult{},
		requestTimer: newTimer(DefaultClock{}),
	}

	_ = processor.materialize()
	_ = processor.parse()
	err := processor.send()

	if err == nil {
		t.Fatal("Expected TLS handshake error, but request succeeded!")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("Expected certificate error, got: %v", err)
	}
}

func TestRequestProcessor_EmptyBodyGet_NoChunkedEncoding(t *testing.T) {
	var receivedTransferEncoding []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTransferEncoding = r.TransferEncoding
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		client:       srv.Client(),
		requestBp:    reqBp,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
		status:       new(ResultStatus),
		result:       &RequestResult{},
		requestTimer: newTimer(DefaultClock{}),
	}

	_ = processor.materialize()
	_ = processor.parse()
	err := processor.send()
	if err != nil {
		t.Fatalf("send() failed: %v", err)
	}

	for _, te := range receivedTransferEncoding {
		if te == "chunked" {
			t.Fatalf("BUG EXPOSED: GET request with no body sent with Transfer-Encoding: chunked!")
		}
	}
}
