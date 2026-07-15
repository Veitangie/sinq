// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
	"golang.org/x/sync/singleflight"
	"hash/maphash"
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
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
		client:       srv.Client(),
	}

	_ = processor.materialize()
	_ = processor.parse()

	done := make(chan error)
	go func() {
		_, err := processor.run()
		done <- err
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

	_, _, _, err := w.runPreScript(scenario.Token{}, dummyExtract, "test.sinq", 1*time.Second)
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
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
		result:       &RequestResult{},
		status:       new(ResultStatus),
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
	}

	resp, err := processor.send()
	if err != nil {
		t.Fatalf("send() failed: %v", err)
	}
	defer resp.Body.Close()

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

	_, _, _, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second)

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
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
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
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
		client:       srv.Client(),
	}

	_ = processor.materialize()
	_ = processor.parse()

	_, err = processor.run()

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
		client:       &http.Client{},
		requestBp:    reqBp,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
		status:       new(ResultStatus),
		result:       &RequestResult{},
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}

	_ = processor.materialize()
	_ = processor.parse()
	err := processor.doRequest()
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
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}

	_ = processor.materialize()
	_ = processor.parse()
	resp, err := processor.send()
	if err != nil {
		t.Fatalf("send() failed: %v", err)
	}
	defer resp.Body.Close()

	for _, te := range receivedTransferEncoding {
		if te == "chunked" {
			t.Fatalf("BUG EXPOSED: GET request with no body sent with Transfer-Encoding: chunked!")
		}
	}
}

func TestRequestProcessor_SingleFlight_CollapsesRequests(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)
	
	// Create shared singleflight group and shared seed just like Runner does
	sg := &singleflight.Group{}
	w.env.singleFlight = sg
	seed := maphash.MakeSeed()

	rawSinq := "GET " + srv.URL + "\n\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			
			// Worker sets up its own hasher internally, simulating what Runner passes by value
			hasher := maphash.Hash{}
			hasher.SetSeed(seed)

			wLocal := setupTestWorker(t, nil)
			wLocal.lc.setupRequestEnvironment(0)
			wLocal.env.singleFlight = sg
			wLocal.env.hasher = hasher
			
			processor := &RequestProcessor{
				ctx:          context.Background(),
				w:            wLocal,
				client:       srv.Client(),
				requestBp:    reqBp,
				scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
				status:       new(ResultStatus),
				result:       &RequestResult{},
				requestTimer: timer.NewTimer(timer.DefaultClock{}),
				singleFlight: true, // Enable SingleFlight
			}

			_ = processor.materialize()
			_ = processor.parse()
			err := processor.doRequest()
			if err != nil {
				t.Errorf("doRequest() failed: %v", err)
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&count) != 1 {
		t.Errorf("Expected server to be hit exactly 1 time, but was hit %d times", count)
	}
}

func TestRequestProcessor_SingleFlight_DistinctRequests(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)
	
	sg := &singleflight.Group{}
	w.env.singleFlight = sg
	seed := maphash.MakeSeed()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			hasher := maphash.Hash{}
			hasher.SetSeed(seed)

			wLocal := setupTestWorker(t, nil)
			wLocal.lc.setupRequestEnvironment(0)
			wLocal.env.singleFlight = sg
			wLocal.env.hasher = hasher

			// Create DISTINCT requests by adding a unique header
			rawSinq := "GET " + srv.URL + "\n" + "X-Unique-ID: " + fmt.Sprint(idx) + "\n\n"
			reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")
			
			processor := &RequestProcessor{
				ctx:          context.Background(),
				w:            wLocal,
				client:       srv.Client(),
				requestBp:    reqBp,
				scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
				status:       new(ResultStatus),
				result:       &RequestResult{},
				requestTimer: timer.NewTimer(timer.DefaultClock{}),
				singleFlight: true,
			}

			_ = processor.materialize()
			_ = processor.parse()
			err := processor.doRequest()
			if err != nil {
				t.Errorf("doRequest() failed: %v", err)
			}
		}(i)
	}

	wg.Wait()

	if atomic.LoadInt32(&count) != 10 {
		t.Errorf("Expected server to be hit exactly 10 times, but was hit %d times", count)
	}
}

func TestRequestProcessor_NoSingleFlight_DoesNotCollapse(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)
	
	sg := &singleflight.Group{}
	w.env.singleFlight = sg
	seed := maphash.MakeSeed()

	rawSinq := "GET " + srv.URL + "\n\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			hasher := maphash.Hash{}
			hasher.SetSeed(seed)

			wLocal := setupTestWorker(t, nil)
			wLocal.lc.setupRequestEnvironment(0)
			wLocal.env.singleFlight = sg
			wLocal.env.hasher = hasher

			processor := &RequestProcessor{
				ctx:          context.Background(),
				w:            wLocal,
				client:       srv.Client(),
				requestBp:    reqBp,
				scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
				status:       new(ResultStatus),
				result:       &RequestResult{},
				requestTimer: timer.NewTimer(timer.DefaultClock{}),
				singleFlight: false, // NO SINGLEFLIGHT
			}

			_ = processor.materialize()
			_ = processor.parse()
			err := processor.doRequest()
			if err != nil {
				t.Errorf("doRequest() failed: %v", err)
			}
		}()
	}

	wg.Wait()

	if atomic.LoadInt32(&count) != 10 {
		t.Errorf("Expected server to be hit exactly 10 times, but was hit %d times", count)
	}
}
