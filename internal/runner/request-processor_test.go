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

	"hash/maphash"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
	lua "github.com/yuin/gopher-lua"
	"net/http/cookiejar"
)

func TestRequestProcessor_ContextCancellationDuringRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	w := setupTestWorker(t, ctx)
	w.lc.SetupRequestEnvironment(0)

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
		err := processor.run()
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
		if processor.result.Status != Aborted {
			t.Errorf("Expected request result status to be Aborted, got %v", processor.result.Status)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("RequestProcessor ignored context cancellation and deadlocked in retry loop")
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
	w.lc.SetupRequestEnvironment(0)

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

	_, err := processor.send()
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
	w.lc.SetupRequestEnvironment(0)

	script := []byte(`req.attach("missing.txt")`)
	extract := func(scenario.Token) []byte { return script }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	_, _, _, _, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second)

	if err == nil {
		t.Fatal("Expected runPreScript to fail on missing file, but it succeeded")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("Expected invalid file path error, got: %v", err)
	}
}

func TestWorker_SkipRequest(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	script := []byte(`req.skip()`)
	extract := func(scenario.Token) []byte { return script }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	_, _, _, skip, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second)

	if err != nil {
		t.Fatalf("Expected runPreScript to succeed, but got error: %v", err)
	}
	if !skip {
		t.Errorf("Expected skip to be true, got false")
	}
}

func TestRequestProcessor_FailsIfBodyAndFileAttached(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

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
	w.lc.SetupRequestEnvironment(0)

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
	w.lc.SetupRequestEnvironment(0)

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
	err := processor.run()
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
	w.lc.SetupRequestEnvironment(0)

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
	_, err := processor.send()
	if err != nil {
		t.Fatalf("send() failed: %v", err)
	}

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
	w.lc.SetupRequestEnvironment(0)

	seed := maphash.MakeSeed()

	rawSinq := "GET " + srv.URL + "\n\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {

			hasher := maphash.Hash{}
			hasher.SetSeed(seed)

			wLocal := setupTestWorker(t, nil)
			wLocal.lc.SetupRequestEnvironment(0)
			wLocal.env.hasher = hasher
			wLocal.env.cachedProcessor = w.env.cachedProcessor

			processor := &RequestProcessor{
				ctx:          context.Background(),
				w:            wLocal,
				client:       srv.Client(),
				requestBp:    reqBp,
				scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
				status:       new(ResultStatus),
				result:       &RequestResult{},
				requestTimer: timer.NewTimer(timer.DefaultClock{}),
				cache:        true,
			}

			_ = processor.materialize()
			_ = processor.parse()
			err := processor.run()
			if err != nil {
				t.Errorf("doRequest() failed: %v", err)
			}
		})
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
	w.lc.SetupRequestEnvironment(0)

	seed := maphash.MakeSeed()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			hasher := maphash.Hash{}
			hasher.SetSeed(seed)

			wLocal := setupTestWorker(t, nil)
			wLocal.lc.SetupRequestEnvironment(0)
			wLocal.env.hasher = hasher

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
				cache:        true,
			}

			_ = processor.materialize()
			_ = processor.parse()
			err := processor.run()
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
	w.lc.SetupRequestEnvironment(0)

	seed := maphash.MakeSeed()

	rawSinq := "GET " + srv.URL + "\n\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {

			hasher := maphash.Hash{}
			hasher.SetSeed(seed)

			wLocal := setupTestWorker(t, nil)
			wLocal.lc.SetupRequestEnvironment(0)
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
				cache:        false,
			}

			_ = processor.materialize()
			_ = processor.parse()
			err := processor.run()
			if err != nil {
				t.Errorf("doRequest() failed: %v", err)
			}
		})
	}

	wg.Wait()

	if atomic.LoadInt32(&count) != 10 {
		t.Errorf("Expected server to be hit exactly 10 times, but was hit %d times", count)
	}
}

func TestRequestProcessor_AssertAndPostScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n$ASSERT{\nsinq.assert.code(200)\n}\n$POST{\nenv.postRan = true\n}"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "hooks.sinq")

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		client:       srv.Client(),
		requestBp:    reqBp,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{ScriptTimeout: scenario.Duration{Duration: 5 * time.Second}, MaxBodySize: config.DataSize{ByteAmount: 1024}}},
		status:       new(ResultStatus),
		result:       &RequestResult{},
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}
	processor.totalRequestTimer = timer.NewTimer(timer.DefaultClock{})

	_ = processor.materialize()
	_ = processor.parse()
	err := processor.run()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	err = processor.runAssert()
	if err != nil {
		t.Fatalf("runAssert failed: %v", err)
	}

	err = processor.runPost()
	if err != nil {
		t.Fatalf("runPost failed: %v", err)
	}

	processor.finalize()
}

func TestRequestProcessor_OversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("this is a very long body that exceeds 10 bytes"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		client:       srv.Client(),
		requestBp:    reqBp,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{MaxBodySize: config.DataSize{ByteAmount: 10}}},
		status:       new(ResultStatus),
		result:       &RequestResult{},
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}

	_ = processor.materialize()
	_ = processor.parse()
	err := processor.run()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	oversized := w.lc.ResponseTable.RawGetString("oversized")
	if oversized.Type() != lua.LTBool || lua.LVAsBool(oversized) != true {
		t.Errorf("Expected oversized flag to be true, got %v", oversized)
	}

	bodyRaw := w.lc.ResponseTable.RawGetString("bodyRaw")
	if bodyRaw.Type() != lua.LTString || len(bodyRaw.String()) > 10 {
		t.Errorf("Expected body to be truncated to 10 bytes, got length %d", len(bodyRaw.String()))
	}
}

func TestRequestProcessor_SaveResponseToFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("saved payload"))
		w.WriteHeader(200)
	}))
	defer srv.Close()

	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	mockFS := fstest.MapFS{}
	w.env.workspace = &mockWorkspace{FS: mockFS}

	rawSinq := "GET " + srv.URL + "\n$PRE{\nreq.saveResponseTo('output.txt')\n}"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	processor := &RequestProcessor{
		ctx:          context.Background(),
		w:            w,
		client:       srv.Client(),
		requestBp:    reqBp,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{ScriptTimeout: scenario.Duration{Duration: 5 * time.Second}, MaxBodySize: config.DataSize{ByteAmount: 1024}}},
		status:       new(ResultStatus),
		result:       &RequestResult{},
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}
	processor.totalRequestTimer = timer.NewTimer(timer.DefaultClock{})

	err := processor.runPre()
	if err != nil {
		t.Fatalf("runPre failed: %v", err)
	}

	_ = processor.materialize()
	_ = processor.parse()
	err = processor.run()
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}

	size := w.lc.ResponseTable.RawGetString("size")
	if size.Type() != lua.LTNumber || lua.LVAsNumber(size) != 13 {
		t.Errorf("Expected size to be 13, got %v", size)
	}
}

func TestRequestProcessor_SingleFlight_ContextIndependence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer srv.Close()

	wGlobal := setupTestWorker(t, nil)
	wGlobal.lc.SetupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n\n"
	reqBp, _ := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "get.sinq")

	seed := maphash.MakeSeed()
	hasher := maphash.Hash{}
	hasher.SetSeed(seed)

	var wg sync.WaitGroup
	wg.Add(2)

	var err1, err2 error
	var resp2 string

	go func() {
		defer wg.Done()

		ctx1, cancel1 := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel1()

		w1 := setupTestWorker(t, nil)
		w1.lc.SetupRequestEnvironment(0)
		w1.env.hasher = hasher
		w1.env.cachedProcessor = wGlobal.env.cachedProcessor

		processor1 := &RequestProcessor{
			ctx:          ctx1,
			w:            w1,
			scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{MaxBodySize: config.DataSize{ByteAmount: 1024}}},
			requestBp:    reqBp,
			materialized: []byte(rawSinq),
			status:       new(ResultStatus),
			result:       &RequestResult{},
			cache:        true,
			client:       http.DefaultClient,
			requestTimer: timer.NewTimer(timer.DefaultClock{}),
		}

		_ = processor1.materialize()
		_ = processor1.parse()
		err1 = processor1.run()
	}()

	go func() {
		defer wg.Done()

		time.Sleep(10 * time.Millisecond)

		ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel2()

		w2 := setupTestWorker(t, nil)
		w2.lc.SetupRequestEnvironment(0)
		w2.env.hasher = hasher
		w2.env.cachedProcessor = wGlobal.env.cachedProcessor

		processor2 := &RequestProcessor{
			ctx:          ctx2,
			w:            w2,
			scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{MaxBodySize: config.DataSize{ByteAmount: 1024}}},
			requestBp:    reqBp,
			materialized: []byte(rawSinq),
			status:       new(ResultStatus),
			result:       &RequestResult{},
			cache:        true,
			client:       http.DefaultClient,
			requestTimer: timer.NewTimer(timer.DefaultClock{}),
		}

		_ = processor2.materialize()
		_ = processor2.parse()
		err2 = processor2.run()

		if err2 == nil {
			bodyRaw := w2.lc.ResponseTable.RawGetString("bodyRaw")
			if bodyRaw.Type() == lua.LTString && bodyRaw.String() == "success" {
				resp2 = "success"
			} else {
				resp2 = fmt.Sprintf("expected success, got type: %v, string: %q", bodyRaw.Type(), bodyRaw.String())
			}
		}
	}()

	wg.Wait()

	if err1 == nil || err1.Error() != "Context aborted while waiting for request to complete" {
		t.Errorf("Expected Worker 1 to abort due to timeout, got: %v", err1)
	}

	if err2 != nil {
		t.Errorf("Expected Worker 2 to succeed, but it failed: %v", err2)
	}

	if resp2 != "success" {
		t.Errorf("Expected Worker 2 to receive 'success', got: %v", resp2)
	}
}

func TestRequestProcessor_CachedRequest_CookiesAndFastPath(t *testing.T) {
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Add("Set-Cookie", "session_id=12345; Path=/")
		w.Header().Add("Set-Cookie", "theme=dark; Path=/")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ctx := context.Background()
	w := setupTestWorker(t, ctx)
	w.lc.SetupRequestEnvironment(0)

	rawSinq := "GET " + srv.URL + "\n"
	reqBp, err := scenario.ParseRequestBlueprint(strings.NewReader(rawSinq), "cache_test.sinq")
	if err != nil {
		t.Fatalf("Failed to parse blueprint: %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}

	client := srv.Client()
	client.Jar = jar

	processor1 := RequestProcessor{
		w:            w,
		ctx:          ctx,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{MaxBodySize: config.DataSize{ByteAmount: 1024}}},
		requestBp:    reqBp,
		materialized: []byte(rawSinq),
		status:       new(ResultStatus),
		result:       &RequestResult{},
		cache:        true,
		client:       client,
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}
	err = processor1.materialize()
	if err != nil {
		t.Fatalf("p1 materialize: %v", err)
	}
	err = processor1.parse()
	if err != nil {
		t.Fatalf("p1 parse: %v", err)
	}
	err = processor1.run()
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}

	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected exactly 1 request to server, got %d", requestCount)
	}

	cookies := jar.Cookies(processor1.httpRequest.URL)
	if len(cookies) != 2 {
		t.Errorf("Expected 2 cookies, got %d", len(cookies))
	}

	jar, _ = cookiejar.New(nil)
	client.Jar = jar

	processor2 := RequestProcessor{
		w:            w,
		ctx:          ctx,
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{MaxBodySize: config.DataSize{ByteAmount: 1024}}},
		requestBp:    reqBp,
		materialized: []byte(rawSinq),
		status:       new(ResultStatus),
		result:       &RequestResult{},
		cache:        true,
		client:       client,
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
	}
	err = processor2.materialize()
	if err != nil {
		t.Fatalf("p2 materialize: %v", err)
	}
	err = processor2.parse()
	if err != nil {
		t.Fatalf("p2 parse: %v", err)
	}
	err = processor2.run()

	if err != nil {
		t.Fatalf("Second request failed: %v", err)
	}

	if atomic.LoadInt32(&requestCount) != 1 {
		t.Errorf("Expected exactly 1 request to server (second should be cached), got %d", requestCount)
	}

	cookies2 := jar.Cookies(processor2.httpRequest.URL)
	if len(cookies2) != 2 {
		t.Errorf("Expected 2 cookies from cache, got %d", len(cookies2))
	}
}
