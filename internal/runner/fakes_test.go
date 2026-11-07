// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing/fstest"
	"time"
)

type sequenceRoundTripper struct {
	bodies []string
	calls  atomic.Int32
}

func (s *sequenceRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	i := int(s.calls.Add(1)) - 1
	body := s.bodies[min(i, len(s.bodies)-1)]
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
}

type heldRoundTripper struct {
	sent    atomic.Int32
	arrived chan struct{}
	release chan struct{}
	once    sync.Once
}

func (h *heldRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	h.sent.Add(1)
	h.once.Do(func() { close(h.arrived) })
	<-h.release
	return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
}

type refusingRoundTripper struct{}

func (refusingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

type bodyRoundTripper struct{ body string }

func (b bodyRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode:    200,
		Body:          io.NopCloser(strings.NewReader(b.body)),
		ContentLength: int64(len(b.body)),
	}, nil
}

type okThenRefusingRoundTripper struct{ calls atomic.Int32 }

func (r *okThenRefusingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	if r.calls.Add(1) == 1 {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("first attempt")), ContentLength: 13}, nil
	}
	return nil, errors.New("connection refused")
}

type refusingThenOkRoundTripper struct{ calls atomic.Int32 }

func (r *refusingThenOkRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	if r.calls.Add(1) == 1 {
		return nil, errors.New("connection refused")
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), ContentLength: 2}, nil
}

type recordingRoundTripper struct {
	mu          sync.Mutex
	contentType string
	body        []byte
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	r.mu.Lock()
	r.contentType = req.Header.Get("Content-Type")
	r.body = body
	r.mu.Unlock()
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("")), ContentLength: 0}, nil
}

type hangingRoundTripper struct{}

const hangingGiveUp = 2 * time.Second

func (hangingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case <-time.After(hangingGiveUp):
		return nil, errors.New("hangingRoundTripper: request context was never cancelled")
	}
}

type bodyFailingRoundTripper struct{ failErr error }

type failingBody struct {
	ctx     context.Context
	failErr error
	sent    bool
}

func (b *failingBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "partial"), nil
	}
	if b.failErr != nil {
		return 0, b.failErr
	}
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-time.After(hangingGiveUp):
		return 0, errors.New("failingBody: request context was never cancelled")
	}
}

func (*failingBody) Close() error { return nil }

func (r bodyFailingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: &failingBody{ctx: req.Context(), failErr: r.failErr}, ContentLength: -1}, nil
}

type memWorkspace struct {
	mu    sync.Mutex
	files fstest.MapFS
}

type memFile struct {
	bytes.Buffer
	ws   *memWorkspace
	name string
}

func (f *memFile) Close() error {
	f.ws.mu.Lock()
	defer f.ws.mu.Unlock()
	f.ws.files[f.name] = &fstest.MapFile{Data: bytes.Clone(f.Bytes())}
	return nil
}

func (m *memWorkspace) Open(name string) (fs.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.files.Open(name)
}

func (m *memWorkspace) Stat(name string) (fs.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fs.Stat(m.files, name)
}

func (m *memWorkspace) MkDirall(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = &fstest.MapFile{Mode: fs.ModeDir}
	return nil
}

func (m *memWorkspace) Create(name string) (io.WriteCloser, error) {
	return &memFile{ws: m, name: name}, nil
}

func (m *memWorkspace) String() string { return "mem" }

type uncreatableWorkspace struct{ mockWorkspace }

func (*uncreatableWorkspace) Create(name string) (io.WriteCloser, error) {
	return nil, errors.New("permission denied: " + name)
}
