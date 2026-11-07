// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/scenario"
)

func mpEntry(e multipartEntry) multipartEntry {
	if e.headers == nil {
		e.headers = map[string][]string{}
	}
	return e
}

func parseMultipart(t *testing.T, body []byte, contentType string) *multipart.Reader {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("returned Content-Type %q is not parseable: %v", contentType, err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("expected multipart/form-data, got %q", mediaType)
	}
	if params["boundary"] == "" {
		t.Fatalf("returned Content-Type %q carries no boundary parameter", contentType)
	}
	return multipart.NewReader(strings.NewReader(string(body)), params["boundary"])
}

func TestMakeMultipartBody_FieldParts(t *testing.T) {
	ws := &mockWorkspace{FS: fstest.MapFS{}}
	data := multipartData{entries: []multipartEntry{
		mpEntry(multipartEntry{contentDisposition: `form-data; name="a"`, data: "1"}),
		mpEntry(multipartEntry{contentDisposition: `form-data; name="b"`, data: "two"}),
	}}

	body, ct, err := makeMultipartBody(data, ws)
	if err != nil {
		t.Fatalf("makeMultipartBody: %v", err)
	}

	mr := parseMultipart(t, body, ct)
	want := []struct{ name, body string }{{"a", "1"}, {"b", "two"}}
	for i, w := range want {
		part, err := mr.NextPart()
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		if part.FormName() != w.name {
			t.Errorf("part %d: name = %q, want %q", i, part.FormName(), w.name)
		}
		got, _ := io.ReadAll(part)
		if string(got) != w.body {
			t.Errorf("part %d: body = %q, want %q", i, got, w.body)
		}
	}
	if _, err := mr.NextPart(); !errors.Is(err, io.EOF) {
		t.Errorf("expected exactly 2 parts, got a 3rd (err=%v)", err)
	}
}

func TestMakeMultipartBody_FileFromSource(t *testing.T) {
	ws := &mockWorkspace{FS: fstest.MapFS{
		"up.bin": &fstest.MapFile{Data: []byte("raw\x00file\xffbytes")},
	}}
	data := multipartData{entries: []multipartEntry{mpEntry(multipartEntry{
		contentDisposition: `form-data; name="f"; filename="up.bin"`,
		contentType:        "application/octet-stream",
		source:             "up.bin",
	})}}

	body, ct, err := makeMultipartBody(data, ws)
	if err != nil {
		t.Fatalf("makeMultipartBody: %v", err)
	}

	part, err := parseMultipart(t, body, ct).NextPart()
	if err != nil {
		t.Fatalf("NextPart: %v", err)
	}
	if part.FileName() != "up.bin" {
		t.Errorf("filename = %q, want up.bin", part.FileName())
	}
	if hdr := part.Header.Get("Content-Type"); hdr != "application/octet-stream" {
		t.Errorf("part Content-Type = %q, want application/octet-stream", hdr)
	}
	got, _ := io.ReadAll(part)
	if string(got) != "raw\x00file\xffbytes" {
		t.Errorf("part body = %q, want the raw file bytes", got)
	}
}

func TestMakeMultipartBody_MissingSourceFileErrors(t *testing.T) {
	ws := &mockWorkspace{FS: fstest.MapFS{}}
	data := multipartData{entries: []multipartEntry{mpEntry(multipartEntry{
		contentDisposition: `form-data; name="f"; filename="nope.txt"`,
		source:             "nope.txt",
	})}}

	if _, _, err := makeMultipartBody(data, ws); err == nil {
		t.Fatal("expected an error when the source file is missing")
	}
}

func TestMakeMultipartBody_CustomBoundary(t *testing.T) {
	ws := &mockWorkspace{FS: fstest.MapFS{}}

	t.Run("honoured", func(t *testing.T) {
		data := multipartData{
			boundary: "SinqBoundary1234",
			entries:  []multipartEntry{mpEntry(multipartEntry{contentDisposition: `form-data; name="a"`, data: "1"})},
		}
		body, ct, err := makeMultipartBody(data, ws)
		if err != nil {
			t.Fatalf("makeMultipartBody: %v", err)
		}
		if !strings.Contains(ct, "boundary=SinqBoundary1234") {
			t.Errorf("Content-Type %q does not carry the custom boundary", ct)
		}
		if !strings.Contains(string(body), "--SinqBoundary1234") {
			t.Errorf("body does not use the custom boundary delimiter")
		}
	})

	t.Run("invalid boundary surfaces an error", func(t *testing.T) {
		data := multipartData{
			boundary: strings.Repeat("x", 71),
			entries:  []multipartEntry{mpEntry(multipartEntry{contentDisposition: `form-data; name="a"`, data: "1"})},
		}
		if _, _, err := makeMultipartBody(data, ws); err == nil {
			t.Fatal("expected an error for an over-long boundary")
		}
	})
}

func TestMakeMultipartBody_EndsWithClosingDelimiter(t *testing.T) {
	ws := &mockWorkspace{FS: fstest.MapFS{}}
	data := multipartData{
		boundary: "SinqBoundary1234",
		entries:  []multipartEntry{mpEntry(multipartEntry{contentDisposition: `form-data; name="a"`, data: "1"})},
	}

	body, _, err := makeMultipartBody(data, ws)
	if err != nil {
		t.Fatalf("makeMultipartBody: %v", err)
	}
	if !strings.HasSuffix(string(body), "\r\n--SinqBoundary1234--\r\n") {
		t.Errorf("body does not end with the closing delimiter, tail: %q", string(body[max(0, len(body)-40):]))
	}
}

func TestMakeMultipartBody_HandlesNilHeaders(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("makeMultipartBody panicked on an entry with nil headers: %v", r)
		}
	}()

	data := multipartData{entries: []multipartEntry{{
		contentDisposition: `form-data; name="a"`,
		data:               "1",
	}}}
	if _, _, err := makeMultipartBody(data, &mockWorkspace{FS: fstest.MapFS{}}); err != nil {
		t.Fatalf("makeMultipartBody: %v", err)
	}
}

func newLuaHeaderTable(t *testing.T, entries map[string]lua.LValue) *lua.LTable {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	tbl := L.NewTable()
	for k, v := range entries {
		tbl.RawSetString(k, v)
	}
	return tbl
}

func TestLTableToHeaders_ValidValues(t *testing.T) {
	tbl := newLuaHeaderTable(t, map[string]lua.LValue{
		"X-Str":  lua.LString("s"),
		"X-Num":  lua.LNumber(7),
		"X-Bool": lua.LTrue,
	})

	h, err := lTableToHeaders(tbl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := h["X-Str"]; len(got) != 1 || got[0] != "s" {
		t.Errorf("X-Str = %v, want [s]", got)
	}
	if got := h["X-Num"]; len(got) != 1 || got[0] != "7" {
		t.Errorf("X-Num = %v, want [7]", got)
	}
	if got := h["X-Bool"]; len(got) != 1 || got[0] != "true" {
		t.Errorf("X-Bool = %v, want [true]", got)
	}
}

func TestLTableToHeaders_InvalidValueTypeErrors(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSetString("X-Bad", L.NewTable())

	if _, err := lTableToHeaders(tbl); err == nil {
		t.Fatal("expected an error for a table-typed header value, got nil")
	}
}

func TestLTableToHeaders_NonStringKeyErrors(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	tbl := L.NewTable()
	tbl.RawSetInt(1, lua.LString("x"))

	if _, err := lTableToHeaders(tbl); err == nil {
		t.Fatal("expected an error for a non-string header key, got nil")
	}
}

func TestLTableToHeaders_CanonicalisesKeys(t *testing.T) {
	tbl := newLuaHeaderTable(t, map[string]lua.LValue{
		"content-type": lua.LString("application/json"),
	})

	h, err := lTableToHeaders(tbl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, raw := h["content-type"]; raw {
		t.Error("header key was stored verbatim; expected it canonicalised to Content-Type")
	}
	if got := h["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Errorf("Content-Type = %v, want [application/json]", got)
	}
}

func runMultipartPre(t *testing.T, w *worker, script string) (preScriptResult, error) {
	t.Helper()
	return runMultipartPreAt(t, w, "test.sinq", script)
}

func runMultipartPreAt(t *testing.T, w *worker, sinqFile, script string) (preScriptResult, error) {
	t.Helper()
	w.lc.SetupRequestEnvironment(0)
	extract := func(scenario.Token) []byte { return []byte(script) }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}
	return w.runPreScript(token, extract, sinqFile, time.Second)
}

func TestMultipartBuilder_SourcePathResolution(t *testing.T) {
	t.Run("resolved relative to the .sinq file", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		w.env.workspace = &mockWorkspace{FS: fstest.MapFS{
			"suite/nested/payload.bin": &fstest.MapFile{Data: []byte("hi")},
		}}
		res, err := runMultipartPreAt(t, w, "suite/nested/req.sinq",
			`req.multipart({ name = "f", source = "payload.bin", filename = true })`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := res.multipart.entries[0].source; got != "suite/nested/payload.bin" {
			t.Errorf("source resolved to %q, want suite/nested/payload.bin", got)
		}
	})

	t.Run("absolute source rejected", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		_, err := runMultipartPre(t, w, `req.multipart({ name = "f", source = "/etc/hosts" })`)
		if err == nil || !strings.Contains(err.Error(), "should not be absolute") {
			t.Fatalf("want an absolute-path rejection, got: %v", err)
		}
	})

	t.Run("missing source rejected at build time", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		w.env.workspace = &mockWorkspace{FS: fstest.MapFS{}}
		_, err := runMultipartPre(t, w, `req.multipart({ name = "f", source = "nope.bin" })`)
		if err == nil || !strings.Contains(err.Error(), "invalid source path") {
			t.Fatalf("want a missing-file rejection, got: %v", err)
		}
	})
}

func TestMultipartBuilder_Rejects(t *testing.T) {
	cases := map[string]struct {
		script string
		errSub string
	}{
		"data and source together": {
			`req.multipart({ name = "a", data = "x", source = "y" })`,
			"both data and source",
		},
		"neither contentDisposition nor name": {
			`req.multipart({ data = "x" })`,
			"neither contentDisposition nor name",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := setupTestWorker(t, nil)
			_, err := runMultipartPre(t, w, tc.script)
			if err == nil || !strings.Contains(err.Error(), tc.errSub) {
				t.Fatalf("want error containing %q, got: %v", tc.errSub, err)
			}
		})
	}
}

func TestMultipartBuilder_FieldVsFilePart(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `
		req.multipart(
			{ name = "plain", data = "1" },
			{ name = "upload", data = "1", filename = "x.txt" }
		)
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.multipart.entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(res.multipart.entries))
	}
	if cd := res.multipart.entries[0].contentDisposition; strings.Contains(cd, "filename") {
		t.Errorf("field part should have no filename param, got %q", cd)
	}
	if cd := res.multipart.entries[1].contentDisposition; !strings.Contains(cd, `filename="x.txt"`) {
		t.Errorf("file part should carry filename=\"x.txt\", got %q", cd)
	}
}

func TestMultipartBuilder_ContentTypeSniffChain(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.workspace = &mockWorkspace{FS: fstest.MapFS{
		"pic.png": &fstest.MapFile{Data: []byte("x")},
	}}

	cases := map[string]struct {
		entry string
		want  string
	}{
		"sniff from filename ext":       {`{ name = "f", data = "x", filename = "photo.png" }`, "image/png"},
		"fall back to source ext":       {`{ name = "f", source = "pic.png", filename = "blob" }`, "image/png"},
		"explicit contentType wins":     {`{ name = "f", data = "x", filename = "photo.png", contentType = "text/plain" }`, "text/plain"},
		"unknown ext -> octet-stream":   {`{ name = "f", data = "x", filename = "mystery.zzz" }`, "application/octet-stream"},
		"no filename -> no contentType": {`{ name = "f", data = "x" }`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := runMultipartPre(t, w, "req.multipart("+tc.entry+")")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := res.multipart.entries[0].contentType; got != tc.want {
				t.Errorf("contentType = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMultipartBuilder_Accumulates(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `
		req.multipart({ name = "a", data = "1" })
		req.multipart({ name = "b", data = "2" }, { name = "c", data = "3" })
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.multipart.entries) != 3 {
		t.Fatalf("want 3 accumulated entries, got %d", len(res.multipart.entries))
	}
}

func TestMultipartBuilder_ArrayTableForm(t *testing.T) {
	cases := map[string]string{
		"single array table": `req.multipart{ { name = "a", data = "1" }, { name = "b", data = "2" } }`,
		"array table via local": `local parts = { { name = "a", data = "1" }, { name = "b", data = "2" } }
		                          req.multipart(parts)`,
		"single-entry array table": `req.multipart{ { name = "a", data = "1" } }`,
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			w := setupTestWorker(t, nil)
			res, err := runMultipartPre(t, w, script)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(res.multipart.entries) == 0 {
				t.Fatal("array-table form produced no entries")
			}
		})
	}
}

func TestMultipartBuilder_MixedTableUsesArrayEntries(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `req.multipart{ name = "dropped", data = "dropped", { name = "kept", data = "1" } }`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.multipart.entries) != 1 {
		t.Fatalf("want 1 entry (the array element), got %d", len(res.multipart.entries))
	}
	if cd := res.multipart.entries[0].contentDisposition; !strings.Contains(cd, `name="kept"`) {
		t.Errorf("expected the array entry to win, got %q", cd)
	}
}

func TestMultipartBuilder_FilenameTrueDerivesFromSource(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.workspace = &mockWorkspace{FS: fstest.MapFS{
		"dir/report.pdf": &fstest.MapFile{Data: []byte("x")},
	}}
	res, err := runMultipartPre(t, w, `req.multipart({ name = "f", source = "dir/report.pdf", filename = true })`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cd := res.multipart.entries[0].contentDisposition
	if !strings.Contains(cd, `filename="report.pdf"`) {
		t.Errorf("filename=true should advertise basename(source); got %q", cd)
	}
	if ct := res.multipart.entries[0].contentType; ct != "application/pdf" {
		t.Errorf("contentType sniffed from source = %q, want application/pdf", ct)
	}
}

func TestMultipartBuilder_FilenameTrueWithoutSource(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `req.multipart({ name = "f", data = "x", filename = true })`)
	if err != nil {
		return
	}
	if cd := res.multipart.entries[0].contentDisposition; strings.Contains(cd, `filename="."`) {
		t.Fatalf("filename=true without source produced %q", cd)
	}
}

func TestMultipartBuilder_WrongTypedFieldRejected(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `req.multipart({ name = "a", data = 123 })`)
	if err == nil && len(res.multipart.entries) == 1 && res.multipart.entries[0].data == "" {
		t.Fatal("numeric data field was silently dropped; want an error (or coercion to \"123\")")
	}
}

func TestMultipartBuilder_BoundaryOverride(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `
		req.multipartBoundary("SinqFixedBoundary")
		req.multipart({ name = "a", data = "1" })
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.multipart.boundary != "SinqFixedBoundary" {
		t.Fatalf("boundary = %q, want SinqFixedBoundary", res.multipart.boundary)
	}
	_, ct, err := makeMultipartBody(res.multipart, &mockWorkspace{FS: fstest.MapFS{}})
	if err != nil {
		t.Fatalf("makeMultipartBody: %v", err)
	}
	if !strings.Contains(ct, "boundary=SinqFixedBoundary") {
		t.Errorf("assembled Content-Type %q does not carry the fixed boundary", ct)
	}
}

func TestMultipartBuilder_PerPartHeadersFlowThrough(t *testing.T) {
	w := setupTestWorker(t, nil)
	res, err := runMultipartPre(t, w, `
		req.multipart({
			name = "chunk",
			data = "x",
			headers = { ["X-Chunk"] = "3", ["X-Count"] = 9, ["X-Last"] = true },
		})
	`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, ct, err := makeMultipartBody(res.multipart, &mockWorkspace{FS: fstest.MapFS{}})
	if err != nil {
		t.Fatalf("makeMultipartBody: %v", err)
	}
	part, err := parseMultipart(t, body, ct).NextPart()
	if err != nil {
		t.Fatalf("NextPart: %v", err)
	}
	for k, want := range map[string]string{"X-Chunk": "3", "X-Count": "9", "X-Last": "true"} {
		if got := part.Header.Get(k); got != want {
			t.Errorf("part header %s = %q, want %q", k, got, want)
		}
	}
}

func TestMultipartBuilder_InvalidHeadersRaise(t *testing.T) {
	w := setupTestWorker(t, nil)
	_, err := runMultipartPre(t, w, `req.multipart({ name = "a", data = "1", headers = { ["X-Bad"] = {} } })`)
	if err == nil || !strings.Contains(err.Error(), "invalid headers") {
		t.Fatalf("want an 'invalid headers' error, got: %v", err)
	}
}

func TestMaterializeMultipart_AssembleOnceAndRetrySafe(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.workspace = &mockWorkspace{FS: fstest.MapFS{}}

	p := &RequestProcessor{
		w:           w,
		httpRequest: &http.Request{Header: http.Header{}},
		multipart: multipartData{entries: []multipartEntry{
			mpEntry(multipartEntry{contentDisposition: `form-data; name="a"`, data: "1"}),
		}},
	}

	if err := p.validateBody(); err != nil {
		t.Fatalf("validateBody (attempt 1): %v", err)
	}
	if err := p.materializeMultipart(); err != nil {
		t.Fatalf("materializeMultipart (attempt 1): %v", err)
	}
	first := string(p.multipart.materialized)
	if first == "" {
		t.Fatal("multipart body was not materialised")
	}
	if p.httpRequest.ContentLength != int64(len(first)) {
		t.Errorf("ContentLength = %d, want %d", p.httpRequest.ContentLength, len(first))
	}

	if err := p.validateBody(); err != nil {
		t.Fatalf("validateBody (attempt 2) tripped after materialisation: %v", err)
	}
	if err := p.materializeMultipart(); err != nil {
		t.Fatalf("materializeMultipart (attempt 2): %v", err)
	}
	if got := string(p.multipart.materialized); got != first {
		t.Error("multipart body was reassembled on the second attempt")
	}
}
