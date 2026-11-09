// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
)

func TestWorker_ProcessStage_MultipartOnTheWire(t *testing.T) {
	cases := []struct {
		name        string
		cache       bool
		contentType string
	}{
		{"plain", false, ""},
		{"cached", true, ""},
		{"own Content-Type is kept", false, "multipart/form-data; boundary=mine"},
		{"own Content-Type is kept, cached", true, "multipart/form-data; boundary=mine"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pre := `req.multipart({ name = "field", data = "value" })`
			if tc.contentType != "" {
				pre += ` req.multipartBoundary("mine")`
			}
			if tc.cache {
				pre += ` req.cache(true)`
			}
			raw := "$PRE{ " + pre + " }\nPOST http://localhost/upload HTTP/1.1\n"
			if tc.contentType != "" {
				raw += "Content-Type: " + tc.contentType + "\n"
			}

			rec := &recordingRoundTripper{}
			res, obs := runSource(t, raw, func(w *worker) {
				w.env.transport = rec
				cachedVia(w)
			})
			if rr := res.StageResults[0]; rr.Status != event.Success {
				t.Fatalf("Expected the request to succeed, got %v (%s)", rr.Status, rr.ErrorMessage)
			}
			if ex := executions(obs); len(ex) != 1 || hasFlag(ex[0], event.CachedMask) != tc.cache {
				t.Fatalf("Expected one Execution with Cached=%v, got %+v", tc.cache, ex)
			}

			rec.mu.Lock()
			defer rec.mu.Unlock()
			if tc.contentType != "" && rec.contentType != tc.contentType {
				t.Errorf("Expected the script's own Content-Type %q to be kept, got %q", tc.contentType, rec.contentType)
			}
			mediaType, params, err := mime.ParseMediaType(rec.contentType)
			if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
				t.Fatalf("Expected a multipart/form-data Content-Type with a boundary, got %q", rec.contentType)
			}
			form, err := multipart.NewReader(bytes.NewReader(rec.body), params["boundary"]).ReadForm(1 << 20)
			if err != nil {
				t.Fatalf("Expected a readable multipart body, got %v: %q", err, rec.body)
			}
			if got := form.Value["field"]; len(got) != 1 || got[0] != "value" {
				t.Errorf("Expected field=value in the sent body, got %v", form.Value)
			}
		})
	}
}

type receivedRequest struct {
	mu               sync.Mutex
	body             []byte
	transferEncoding []string
}

func (r *receivedRequest) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.body, r.transferEncoding = body, req.TransferEncoding
		r.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWorker_ProcessStage_AttachedFileIsStreamedChunked(t *testing.T) {
	got := &receivedRequest{}
	srv := got.server(t)
	ws := &mockWorkspace{FS: fstest.MapFS{"upload.txt": &fstest.MapFile{Data: []byte("hello chunked world")}}}

	res, _ := runSourceIn(t, "$PRE{ req.attach(\"upload.txt\") }\nPOST "+srv.URL+"/upload HTTP/1.1\n", ws, nil, viaTransport(srv.Client().Transport))

	if rr := res.StageResults[0]; rr.Status != event.Success {
		t.Fatalf("Expected the upload to succeed, got %v (%s)", rr.Status, rr.ErrorMessage)
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	if string(got.body) != "hello chunked world" {
		t.Errorf("Expected the server to receive the attached file, got %q", got.body)
	}
	if !slices.Contains(got.transferEncoding, "chunked") {
		t.Errorf("Expected an attached file to be streamed with Transfer-Encoding: chunked, got %v", got.transferEncoding)
	}
}

func TestWorker_ProcessStage_LiteralBodyIsSent(t *testing.T) {
	got := &receivedRequest{}
	srv := got.server(t)

	res, _ := runSource(t, "POST "+srv.URL+"/ HTTP/1.1\n\nhello body", viaTransport(srv.Client().Transport))

	if rr := res.StageResults[0]; rr.Status != event.Success {
		t.Fatalf("Expected the request to succeed, got %v (%s)", rr.Status, rr.ErrorMessage)
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	if string(got.body) != "hello body" {
		t.Errorf("Expected the server to receive the literal body, got %q", got.body)
	}
}

func TestWorker_ProcessStage_GetWithoutBodyIsNotChunked(t *testing.T) {
	got := &receivedRequest{}
	srv := got.server(t)

	res, _ := runSource(t, "GET "+srv.URL+"/ HTTP/1.1\n\n", viaTransport(srv.Client().Transport))

	if rr := res.StageResults[0]; rr.Status != event.Success {
		t.Fatalf("Expected the request to succeed, got %v (%s)", rr.Status, rr.ErrorMessage)
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	if slices.Contains(got.transferEncoding, "chunked") {
		t.Errorf("Expected a GET without a body not to be sent chunked, got %v", got.transferEncoding)
	}
}

func TestWorker_ProcessStage_AttachAndLiteralBodyConflict(t *testing.T) {
	ws := &mockWorkspace{FS: fstest.MapFS{"attached_file.txt": &fstest.MapFile{Data: []byte("file")}}}
	cases := []struct {
		name     string
		raw      string
		conflict bool
	}{
		{"literal body", "$PRE{ req.attach(\"attached_file.txt\") }\nPOST http://localhost/ HTTP/1.1\n\nthis is a body", true},
		{"empty body", "$PRE{ req.attach(\"attached_file.txt\") }\nGET http://localhost/ HTTP/1.1\n\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := runSourceIn(t, tc.raw, ws, nil)

			rr := res.StageResults[0]
			if tc.conflict && (rr.Status != event.Error || !strings.Contains(rr.ErrorMessage, "both body and a file source")) {
				t.Errorf("Expected a body and an attached file to conflict, got %v %q", rr.Status, rr.ErrorMessage)
			}
			if !tc.conflict && rr.Status != event.Success {
				t.Errorf("Expected an attached file with an empty body to be fine, got %v %q", rr.Status, rr.ErrorMessage)
			}
		})
	}
}

func TestWorker_ProcessStage_BodyIsTruncatedAtMaxBody(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		oversized bool
		want      string
	}{
		{"over the limit", "this is a very long body that exceeds 10 bytes", true, "this is a "},
		{"exactly at the limit", "1234567890", false, "1234567890"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := "GET http://localhost/ HTTP/1.1\n\n$ASSERT{\n" +
				"  sinq.assert.equals(res.oversized, " + fmt.Sprint(tc.oversized) + ", \"oversized\")\n" +
				"  sinq.assert.equals(res.bodyRaw, \"" + tc.want + "\", \"bodyRaw\")\n" +
				"}\n"
			res, _ := runSourceWith(t, raw,
				func(cfg *scenario.ScenarioConfig) { cfg.MaxBodySize.ByteAmount = 10 },
				viaTransport(bodyRoundTripper{body: tc.body}))

			if rr := res.StageResults[0]; rr.Status != event.Success {
				t.Errorf("Expected oversized=%v and bodyRaw=%q, got %v %v %s", tc.oversized, tc.want, rr.Status, rr.FailedAssertions, rr.ErrorMessage)
			}
		})
	}
}

func TestWorker_ProcessStage_SaveResponseToFile(t *testing.T) {
	ws := &memWorkspace{files: fstest.MapFS{}}
	raw := "$PRE{ req.saveResponseTo(\"output.txt\") }\nGET http://localhost/ HTTP/1.1\n\n" +
		"$ASSERT{\n  sinq.assert.equals(res.size, 13, \"size\")\n  sinq.assert.equals(res.bodyRaw, nil, \"bodyRaw\")\n}\n"

	res, _ := runSourceIn(t, raw, ws, nil, viaTransport(bodyRoundTripper{body: "saved payload"}))

	if rr := res.StageResults[0]; rr.Status != event.Success {
		t.Fatalf("Expected res.size 13 and no buffered body, got %v %v %s", rr.Status, rr.FailedAssertions, rr.ErrorMessage)
	}
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if f, ok := ws.files["output.txt"]; !ok || string(f.Data) != "saved payload" {
		t.Errorf("Expected output.txt to hold the response, got %v", ws.files)
	}
}

func TestWorker_ProcessStage_ExecutionSizeOfSavedResponse(t *testing.T) {
	ws := &memWorkspace{files: fstest.MapFS{}}
	raw := "$PRE{ req.saveResponseTo(\"output.txt\") }\nGET http://localhost/ HTTP/1.1\n"

	_, obs := runSourceIn(t, raw, ws, nil, viaTransport(bodyRoundTripper{body: "saved payload"}))

	ex := executions(obs)
	if len(ex) != 1 {
		t.Fatalf("Expected one Execution event, got %d", len(ex))
	}
	if ex[0].Size != 13 {
		t.Errorf("Expected Size to be the 13 bytes written to the file, got %d", ex[0].Size)
	}
}

func TestWorker_ProcessStage_InlineScriptWithoutValueErrors(t *testing.T) {
	res, _ := runSource(t, "GET http://localhost/ HTTP/1.1\nX-Empty: ${ }\n")

	rr := res.StageResults[0]
	if rr.Status != event.Error {
		t.Errorf("Expected an inline script without a value to error, got %v", rr.Status)
	}
	if !strings.Contains(rr.ErrorMessage, "produced no value") {
		t.Errorf("Expected the no-value error, got %q", rr.ErrorMessage)
	}
}

func TestWorker_ProcessStage_FileMatches(t *testing.T) {
	const chunk = 32 << 10
	big := strings.Repeat("0123456789abcdef", 5*chunk/16)

	cases := []struct {
		name     string
		served   string
		expected string
		match    bool
	}{
		{"same content", "hello", "hello", true},
		{"same length, different content", "hello", "hellO", false},
		{"response shorter", "hell", "hello", false},
		{"response longer", "hello!", "hello", false},
		{"several chunks, last byte differs", big, big[:len(big)-1] + "X", false},
		{"exactly one chunk vs one byte more", big[:chunk], big[:chunk+1], false},
		{"one byte more vs exactly one chunk", big[:chunk+1], big[:chunk], false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := setupTestWorker(t, nil)
			w.env.transport = bodyRoundTripper{body: tc.served}
			obs := observe(t, w)

			raw := "$PRE{ req.saveResponseTo(\"out/got.bin\") }\nGET http://localhost/ HTTP/1.1\n\n$ASSERT{ sinq.assert.fileMatches(\"expected.bin\") }\n"
			bps, err := scenario.ParseStageBlueprints(strings.NewReader(raw), "events.sinq", "")
			if err != nil {
				t.Fatalf("Failed to parse requests: %v", err)
			}
			w.processScenario(context.Background(), taskBundle{
				Config: &scenario.ScenarioConfig{
					Name:          "Events",
					ReqTimeout:    scenario.Duration{Duration: time.Second},
					ScriptTimeout: scenario.Duration{Duration: time.Second},
					MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
				},
				Stages:    bps,
				workspace: &memWorkspace{files: fstest.MapFS{"expected.bin": &fstest.MapFile{Data: []byte(tc.expected)}}},
				run:       true,
			})
			rr := obs.result().StageResults[0]

			if tc.match && rr.Status != event.Success {
				t.Errorf("Expected matching files to pass, got %v (%s) %v", rr.Status, rr.ErrorMessage, rr.FailedAssertions)
			}
			if !tc.match && (rr.Status != event.Failure || !slices.Contains(rr.FailedAssertions, "Saved response and file did not match")) {
				t.Errorf("Expected a mismatch failure, got %v (%s) %v", rr.Status, rr.ErrorMessage, rr.FailedAssertions)
			}
		})
	}
}
