// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/sync/singleflight"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
)

func TestWorker_ProcessError_StatusFromError(t *testing.T) {
	cases := []struct {
		err  error
		want event.ResultStatus
	}{
		{context.Canceled, event.Aborted},
		{context.DeadlineExceeded, event.Error},
		{errors.New("boom"), event.Error},
	}
	for _, tc := range cases {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)
		(&RequestProcessor{w: w}).processError(context.Background(), tc.err, 0)
		ends := obs.ofKind(event.StageEnd)
		if len(ends) != 1 || ends[0].Status != tc.want {
			t.Errorf("%v: expected one StageEnd with status %v, got %+v", tc.err, tc.want, ends)
		}
	}
}

func TestWorker_ProcessError_CancelHasNoErrorMessage(t *testing.T) {
	w := setupTestWorker(t, nil)
	obs := observe(t, w)
	(&RequestProcessor{w: w}).processError(context.Background(), context.Canceled, 0)
	if ends := obs.ofKind(event.StageEnd); len(ends) != 1 || ends[0].ErrorString != "" {
		t.Errorf("Expected an aborted StageEnd without an error message, got %+v", ends)
	}
}

func TestWorker_AttachInvalidFileFastFails(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupStageEnvironment(0, true)

	script := []byte(`req.attach("missing.txt")`)
	extract := func(scenario.Token) []byte { return script }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	_, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second, false)

	if err == nil {
		t.Fatal("Expected runPreScript to fail on missing file, but it succeeded")
	}
	if !strings.Contains(err.Error(), "invalid file path") {
		t.Errorf("Expected invalid file path error, got: %v", err)
	}
}

func TestWorker_SkipStage(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupStageEnvironment(0, true)

	script := []byte(`req.skip()`)
	extract := func(scenario.Token) []byte { return script }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	res, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second, false)

	if err != nil {
		t.Fatalf("Expected runPreScript to succeed, but got error: %v", err)
	}
	if !res.skip {
		t.Errorf("Expected skip to be true, got false")
	}
}

func TestCachedRequestProcessor_UsesBody(t *testing.T) {
	var receivedBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL, nil)

	processor := &cachedRequestProcessor{
		group:        singleflight.Group{},
		ctx:          context.Background(),
		transport:    http.DefaultTransport,
		maxCacheSize: config.DataSize{ByteAmount: 1024, Unit: config.MiByte},
		cacheTimeout: 5 * time.Second,
		eb:           newTestBus(t),
	}

	getBody := func(ws Workspace) func() (io.ReadCloser, error) {
		return func() (io.ReadCloser, error) {
			return io.NopCloser(io.Reader(bytes.NewBufferString("test body"))), nil
		}
	}

	resCh := processor.process("some-hash", *req, "", getBody, nil, 1)
	res := <-resCh
	if res.Err != nil {
		t.Fatalf("processor.process failed: %v", res.Err)
	}

	if receivedBody != "test body" {
		t.Errorf("Expected body 'test body', got '%s'", receivedBody)
	}
}

func TestRequestProcessor_IsEmptyShortCircuit(t *testing.T) {
	ctx := context.Background()
	w := setupTestWorker(t, ctx)
	w.lc.SetupStageEnvironment(0, true)

	reqBp := &scenario.StageBlueprint{
		Pre:    scenario.Token{PayloadStart: 10, PayloadEnd: 11},
		Assert: scenario.Token{PayloadStart: 20, PayloadEnd: 21},
		Post:   scenario.Token{PayloadStart: 30, PayloadEnd: 31},
		Retry:  scenario.Token{PayloadStart: 40, PayloadEnd: 41},
	}

	scenarioBp := &scenario.ScenarioBlueprint{
		Config: &scenario.ScenarioConfig{},
	}

	processor := newTestProcessor(w)
	processor.ctx = ctx
	processor.scenarioBp = scenarioBp
	processor.stageBp = reqBp

	if err := processor.runPre(); err != nil {
		t.Errorf("Expected nil error for empty Pre block, got %v", err)
	}
	if err := processor.runAssert(); err != nil {
		t.Errorf("Expected nil error for empty Assert block, got %v", err)
	}
	if err := processor.runPost(); err != nil {
		t.Errorf("Expected nil error for empty Post block, got %v", err)
	}
	if err := processor.runRetry(); err != nil {
		t.Errorf("Expected nil error for empty Retry block, got %v", err)
	}
}

func TestRequestProcessor_LogsFailuresAtEachStage(t *testing.T) {
	scriptTimeoutCfg := &scenario.ScenarioConfig{ScriptTimeout: scenario.Duration{Duration: time.Second}}

	t.Run("runPre logs failure", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)
		w.lc.SetupStageEnvironment(0, true)

		reqBp, err := scenario.ParseStageBlueprints(strings.NewReader("GET http://localhost/\n$PRE{\nif then\n}"), "pre_fail.sinq", "")
		if err != nil {
			t.Fatalf("failed to parse blueprint: %v", err)
		}

		p := newTestProcessor(w)
		p.scenarioBp = &scenario.ScenarioBlueprint{Config: scriptTimeoutCfg}
		p.stageBp = reqBp[0]

		_ = p.runPre()
		if !obs.logged("Pre script failed") {
			t.Errorf("expected a 'Pre script failed' debug log line, got: %+v", obs.ofKind(event.Log))
		}
	})

	t.Run("materialize logs failure", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		reqBp := &scenario.StageBlueprint{
			Source: []byte("x"),
			Content: []scenario.Token{
				{Type: scenario.Text, PayloadStart: 0, PayloadEnd: 1},
			},
		}

		p := newTestProcessor(w)
		p.ctx = ctx
		p.scenarioBp = &scenario.ScenarioBlueprint{Config: scriptTimeoutCfg}
		p.stageBp = reqBp

		_ = p.materialize()
		if !obs.logged("Request materialization failed") {
			t.Errorf("expected a materialization-failure debug log line, got: %+v", obs.ofKind(event.Log))
		}
	})

	t.Run("parse logs failure", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)
		w.lc.SetupStageEnvironment(0, true)

		p := newTestProcessor(w)
		p.materialized = []byte("POST http://localhost\n\nthis is a body")
		p.filenameFrom = "attached_file.txt"

		_ = p.parse()
		if !obs.logged("Request parsing failed") {
			t.Errorf("expected a parsing-failure debug log line, got: %+v", obs.ofKind(event.Log))
		}
	})

	t.Run("runRetry logs failure", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)
		w.lc.SetupStageEnvironment(0, true)

		reqBp, err := scenario.ParseStageBlueprints(strings.NewReader("GET http://localhost/\n$RETRY{\nreturn \"not a number\"\n}"), "retry_fail.sinq", "")
		if err != nil {
			t.Fatalf("failed to parse blueprint: %v", err)
		}

		p := newTestProcessor(w)
		p.scenarioBp = &scenario.ScenarioBlueprint{Config: scriptTimeoutCfg}
		p.stageBp = reqBp[0]

		_ = p.runRetry()
		if !obs.logged("Retry script failed") {
			t.Errorf("expected a 'Retry script failed' debug log line, got: %+v", obs.ofKind(event.Log))
		}
	})

	t.Run("runAssert logs failure", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)
		w.lc.SetupStageEnvironment(0, true)

		reqBp, err := scenario.ParseStageBlueprints(strings.NewReader("GET http://localhost/\n$ASSERT{\nif then\n}"), "assert_fail.sinq", "")
		if err != nil {
			t.Fatalf("failed to parse blueprint: %v", err)
		}

		p := newTestProcessor(w)
		p.scenarioBp = &scenario.ScenarioBlueprint{Config: scriptTimeoutCfg}
		p.stageBp = reqBp[0]

		_ = p.runAssert()
		if !obs.logged("Assert script failed") {
			t.Errorf("expected an 'Assert script failed' debug log line, got: %+v", obs.ofKind(event.Log))
		}
	})

	t.Run("runPost logs failure", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		obs := observe(t, w)
		w.lc.SetupStageEnvironment(0, true)

		reqBp, err := scenario.ParseStageBlueprints(strings.NewReader("GET http://localhost/\n$POST{\nerror(\"boom\")\n}"), "post_fail.sinq", "")
		if err != nil {
			t.Fatalf("failed to parse blueprint: %v", err)
		}

		p := newTestProcessor(w)
		p.scenarioBp = &scenario.ScenarioBlueprint{Config: scriptTimeoutCfg}
		p.stageBp = reqBp[0]

		_ = p.runPost()
		if !obs.logged("Post script failed") {
			t.Errorf("expected a 'Post script failed' debug log line, got: %+v", obs.ofKind(event.Log))
		}
	})
}

func TestRequestProcessor_GetHash_CachesResult(t *testing.T) {
	w := setupTestWorker(t, nil)
	p := newTestProcessor(w)
	p.materialized = []byte("some content")
	p.httpRequest = &http.Request{URL: &url.URL{}}

	first := p.getCacheKey()
	if first == "" {
		t.Fatal("expected a non-empty computed hash")
	}

	p.cacheKey = "preset-value"
	second := p.getCacheKey()
	if second != "preset-value" {
		t.Errorf("expected the already-set hash to be returned unchanged, got %q", second)
	}
}

func TestRequestProcessor_GetHash_IncludesMultipartHeaders(t *testing.T) {
	w := setupTestWorker(t, nil)

	mk := func(headerVal string) string {
		p := newTestProcessor(w)
		p.materialized = []byte("POST http://x/\n\n")
		p.httpRequest = &http.Request{URL: &url.URL{}}
		p.multipart = multipartData{entries: []multipartEntry{{
			contentDisposition: `form-data; name="a"`,
			data:               "1",
			headers:            map[string][]string{"X-Sig": {headerVal}},
		}}}
		return p.getCacheKey()
	}

	if mk("AAA") == mk("BBB") {
		t.Fatal("multipart requests differing only by a per-part header hash identically")
	}
}

func TestRequestProcessor_Send_ConflictingBodyAndFile(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupStageEnvironment(0, true)

	req, _ := http.NewRequest("POST", "http://example.invalid/", strings.NewReader("body content"))
	req.ContentLength = 12

	processor := newTestProcessor(w)
	processor.httpRequest = req
	processor.filenameFrom = "attached.txt"
	processor.requestBody = []byte("body content")

	_, err := processor.send()
	if err == nil || !strings.Contains(err.Error(), "more than one body") {
		t.Errorf("expected a conflict error for attached file + body content, got: %v", err)
	}
}

func TestRequestProcessor_SendCached_ConflictingBodyAndFile(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupStageEnvironment(0, true)

	req, _ := http.NewRequest("POST", "http://example.invalid/", nil)
	req.ContentLength = 12

	processor := newTestProcessor(w)
	processor.httpRequest = req
	processor.filenameFrom = "attached.txt"
	processor.requestBody = []byte("body content")

	_, err := processor.sendCached()
	if err == nil || !strings.Contains(err.Error(), "more than one body") {
		t.Errorf("expected a conflict error for attached file + body content in the cached path, got: %v", err)
	}
}

func TestRequestProcessor_SendCached_FileAttachment_SetsUnknownContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	mockFS := fstest.MapFS{"upload.txt": &fstest.MapFile{Data: []byte("cached file body")}}
	w := setupTestWorker(t, nil)
	w.env.workspace = &mockWorkspace{FS: mockFS}
	w.lc.SetupStageEnvironment(0, true)

	req, _ := http.NewRequest("POST", srv.URL, nil)

	processor := newTestProcessor(w)
	processor.httpRequest = req
	processor.client = srv.Client()
	processor.filenameFrom = "upload.txt"
	processor.scenarioBp.Config.MaxBodySize = config.DataSize{ByteAmount: 1024}

	_, err := processor.sendCached()
	if err != nil {
		t.Fatalf("sendCached failed: %v", err)
	}
	if processor.httpRequest.ContentLength != -1 {
		t.Errorf("expected ContentLength to be set to -1 for a streamed file attachment, got %d", processor.httpRequest.ContentLength)
	}
}
