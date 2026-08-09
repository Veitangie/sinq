// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Veitangie/sinq/internal/config"
)

func newTestCachedRequestProcessor(t *testing.T, transport http.RoundTripper) *cachedRequestProcessor {
	t.Helper()
	return &cachedRequestProcessor{
		ctx:          context.Background(),
		transport:    transport,
		maxCacheSize: config.DataSize{ByteAmount: 1 << 20, Unit: config.MiByte},
		cacheTimeout: 5 * time.Second,
		logger:       slog.Default(),
	}
}

func closedBody(body string) func(Workspace) func() (io.ReadCloser, error) {
	return func(Workspace) func() (io.ReadCloser, error) {
		return func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(body)), nil }
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type erroringReadCloser struct {
	remaining []byte
	failWith  error
}

func (e *erroringReadCloser) Read(p []byte) (int, error) {
	if len(e.remaining) == 0 {
		return 0, e.failWith
	}
	n := copy(p, e.remaining)
	e.remaining = e.remaining[n:]
	return n, nil
}

func (e *erroringReadCloser) Close() error { return nil }

type erroringWorkspace struct {
	mockWorkspace
	createErr error
}

func (w *erroringWorkspace) Create(name string) (io.WriteCloser, error) {
	return nil, w.createErr
}

type failingWriteCloser struct{ err error }

func (f failingWriteCloser) Write(p []byte) (int, error) { return 0, f.err }
func (f failingWriteCloser) Close() error                { return nil }

type writeFailWorkspace struct{ mockWorkspace }

func (w *writeFailWorkspace) Create(name string) (io.WriteCloser, error) {
	return failingWriteCloser{err: errors.New("disk full")}, nil
}

func TestSingleflightSafe_RecoversPanic(t *testing.T) {
	safe := singleflightSafe(func() (any, error) {
		panic("boom")
	})

	result, err := safe()
	if result != nil {
		t.Errorf("expected nil result after a recovered panic, got %v", result)
	}
	if err == nil {
		t.Fatal("expected an error after a recovered panic, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected the panic value to be included in the error, got %q", err.Error())
	}
}

func TestSingleflightSafe_PassesThroughNormalResult(t *testing.T) {
	safe := singleflightSafe(func() (any, error) {
		return "value", nil
	})

	result, err := safe()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "value" {
		t.Errorf("expected %q, got %v", "value", result)
	}
}

func TestCachedRequestProcessor_PanicDuringProcess(t *testing.T) {
	sp := newTestCachedRequestProcessor(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("transport should not be reached when getBody panics")
		return nil, nil
	}))

	panicking := func(Workspace) func() (io.ReadCloser, error) {
		panic("getBody exploded")
	}

	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	ws := &mockWorkspace{FS: fstest.MapFS{}}

	ch := sp.process("hash-panic", *req, "", panicking, ws)
	res := <-ch
	if res.Err == nil {
		t.Fatal("expected an error surfaced from the recovered panic, got nil")
	}
	if !strings.Contains(res.Err.Error(), "getBody exploded") {
		t.Errorf("expected the panic message to be included, got %q", res.Err.Error())
	}
}

func TestCachedRequestProcessor_CacheHit(t *testing.T) {
	var calls int32
	var mu sync.Mutex
	sp := newTestCachedRequestProcessor(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader("hi"))}, nil
	}))

	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	ws := &mockWorkspace{FS: fstest.MapFS{}}

	first := <-sp.process("hash-cache", *req, "", closedBody(""), ws)
	if first.Err != nil {
		t.Fatalf("first request failed: %v", first.Err)
	}

	second := <-sp.process("hash-cache", *req, "", closedBody(""), ws)
	if second.Err != nil {
		t.Fatalf("second (cached) request failed: %v", second.Err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Errorf("expected exactly 1 network call for a cache hit, got %d", calls)
	}
}

func TestCachedRequestProcessor_GetBodyError(t *testing.T) {
	sp := newTestCachedRequestProcessor(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatal("transport should not be reached when getBody fails")
		return nil, nil
	}))

	failingBody := func(Workspace) func() (io.ReadCloser, error) {
		return func() (io.ReadCloser, error) { return nil, errors.New("could not open attached file") }
	}

	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	ws := &mockWorkspace{FS: fstest.MapFS{}}

	res := <-sp.process("hash-getbody-err", *req, "", failingBody, ws)
	if res.Err == nil {
		t.Fatal("expected an error when getBody fails, got nil")
	}
}

func TestCachedRequestProcessor_TransportError(t *testing.T) {
	sp := newTestCachedRequestProcessor(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}))

	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	ws := &mockWorkspace{FS: fstest.MapFS{}}

	res := <-sp.process("hash-transport-err", *req, "", closedBody(""), ws)
	if res.Err == nil {
		t.Fatal("expected an error when the transport fails, got nil")
	}
}

func TestMakeIntermediate_OversizedBodyIsTruncated(t *testing.T) {
	body := bytes.Repeat([]byte("a"), 100)
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	ws := &mockWorkspace{FS: fstest.MapFS{}}
	result, err := makeIntermediate(ws, "", 10, resp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.oversized {
		t.Error("expected result.oversized to be true")
	}
	if len(result.body) != 10 {
		t.Errorf("expected body truncated to 10 bytes, got %d", len(result.body))
	}
}

func TestMakeIntermediate_SaveToFile(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Proto:      "HTTP/1.1",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("saved to disk")),
	}

	ws := &mockWorkspace{FS: fstest.MapFS{}}
	result, err := makeIntermediate(ws, "out.bin", 1<<20, resp)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.filenameTo != "out.bin" {
		t.Errorf("expected filenameTo to be set, got %q", result.filenameTo)
	}
	if result.size != int64(len("saved to disk")) {
		t.Errorf("expected size %d, got %d", len("saved to disk"), result.size)
	}
}

func TestMakeIntermediate_CreateFileError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("body")),
	}

	ws := &erroringWorkspace{createErr: errors.New("permission denied")}
	_, err := makeIntermediate(ws, "out.bin", 1<<20, resp)
	if err == nil {
		t.Fatal("expected an error when the workspace fails to create the destination file")
	}
}

func TestMakeIntermediate_CopyToFileError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("body that fails to write")),
	}

	ws := &writeFailWorkspace{}
	_, err := makeIntermediate(ws, "out.bin", 1<<20, resp)
	if err == nil {
		t.Fatal("expected an error when writing the response body to disk fails")
	}
}

func TestMakeIntermediate_ReadBodyError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       &erroringReadCloser{remaining: []byte("partial"), failWith: errors.New("connection reset")},
	}

	ws := &mockWorkspace{FS: fstest.MapFS{}}
	_, err := makeIntermediate(ws, "", 1<<20, resp)
	if err == nil {
		t.Fatal("expected an error when reading the response body fails")
	}
}
