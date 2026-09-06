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
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"veitangie.dev/sinq/internal/luapi"
	"veitangie.dev/sinq/internal/scenario"
)

func TestWorker_ToLuaValue(t *testing.T) {
	w := setupTestWorker(t, nil)

	tests := []struct {
		name     string
		input    any
		validate func(*testing.T, lua.LValue)
	}{
		{
			name:  "HTTP Header Conversion (Multiple, Single, Empty)",
			input: http.Header{"Set-Cookie": []string{"a=1", "b=2"}, "X-Single": []string{"val"}, "X-Empty": []string{}},
			validate: func(t *testing.T, val lua.LValue) {
				tbl, ok := val.(*lua.LTable)
				if !ok {
					t.Fatalf("Expected table, got %v", val.Type())
				}

				cookies := tbl.RawGetString("Set-Cookie")
				if cookies.Type() != lua.LTTable {
					t.Errorf("Expected array table for multiple headers, got %v", cookies.Type())
				}

				single := tbl.RawGetString("X-Single")
				if single.String() != "val" {
					t.Errorf("Expected unwrapped string for single header, got %v", single.String())
				}

				empty := tbl.RawGetString("X-Empty")
				if empty.String() != "" {
					t.Errorf("Expected empty string for 0-length header array")
				}
			},
		},
		{
			name:  "Deeply Nested JSON Map",
			input: map[string]any{"data": map[string]any{"items": []any{1, "two", true}}},
			validate: func(t *testing.T, val lua.LValue) {
				tbl := val.(*lua.LTable)
				dataTbl := tbl.RawGetString("data").(*lua.LTable)
				itemsTbl := dataTbl.RawGetString("items").(*lua.LTable)

				if itemsTbl.RawGetInt(1).(lua.LNumber) != 1 {
					t.Errorf("Failed to translate nested array number")
				}
				if itemsTbl.RawGetInt(2).String() != "two" {
					t.Errorf("Failed to translate nested array string")
				}
				if itemsTbl.RawGetInt(3).(lua.LBool) != true {
					t.Errorf("Failed to translate nested array boolean")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := luapi.ToLuaValue(tt.input, w.lc.LState)
			tt.validate(t, got)
		})
	}
}

func TestWorker_ExecuteAndExtractValue_ImplicitReturn(t *testing.T) {
	w := setupTestWorker(t, nil)

	tests := []struct {
		name        string
		script      string
		wantType    lua.LValueType
		wantString  string
		expectError bool
		errContains string
	}{
		{
			name:       "Explicit Return",
			script:     `return "explicit"`,
			wantType:   lua.LTString,
			wantString: "explicit",
		},
		{
			name:       "Implicit Return Fallback",
			script:     `"implicit"`,
			wantType:   lua.LTString,
			wantString: "implicit",
		},
		{
			name:        "Hard Syntax Error",
			script:      `if then end`,
			expectError: true,
			errContains: "Failed to execute lua script",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := scenario.Token{Type: scenario.Script, Name: tt.name}
			extract := func(scenario.Token) []byte { return []byte(tt.script) }

			val, err := w.executeAndExtractValue(token, extract, "test.sinq", 1*time.Second)

			if tt.expectError {
				if err == nil {
					t.Fatalf("Expected error containing %q, got success", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Expected error to contain %q, got %v", tt.errContains, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if val.Type() != tt.wantType {
				t.Errorf("Expected type %v, got %v", tt.wantType, val.Type())
			}
			if val.String() != tt.wantString {
				t.Errorf("Expected %q, got %q", tt.wantString, val.String())
			}
		})
	}
}

func TestWorker_LuaInfiniteLoop_AbortsOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	w := setupTestWorker(t, ctx)

	script := []byte(`while true do end`)
	extract := func(scenario.Token) []byte { return script }
	token := scenario.Token{Type: scenario.Script, Name: "INFINITE_LOOP"}

	done := make(chan error, 1)
	go func() {
		_, err := w.executeAndExtractValue(token, extract, "test.sinq", 100*time.Millisecond)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Expected context deadline/timeout error, got nil")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Worker deadlocked! The Lua VM did not respect the Go context timeout.")
	}
}

func TestWorker_LuaBuiltinFailures(t *testing.T) {
	w := setupTestWorker(t, nil)

	t.Run("setRequestIdx without args", func(t *testing.T) {
		w.lc.Push(w.lc.NewClosure(w.setRequestIdx))
		err := w.lc.PCall(0, 0, nil)
		if err == nil {
			t.Error("Expected setRequestIdx to fail when missing arguments")
		}
	})

	t.Run("setRequestIdx with string", func(t *testing.T) {
		w.lc.Push(w.lc.NewClosure(w.setRequestIdx))
		w.lc.Push(lua.LString("not_a_number"))
		err := w.lc.PCall(1, 0, nil)
		if err == nil {
			t.Error("Expected setRequestIdx to fail when receiving a string")
		}
	})

	t.Run("failAssert without args", func(t *testing.T) {
		w.lc.Push(w.lc.NewClosure(w.failAssert))
		err := w.lc.PCall(0, 0, nil)
		if err == nil {
			t.Error("Expected failAssert to fail when missing reason string")
		}
	})
}

func TestWorker_RunRetryScript_TypeFailures(t *testing.T) {
	w := setupTestWorker(t, nil)

	extract := func(scenario.Token) []byte { return []byte(`return "string_instead_of_number"`) }
	token := scenario.Token{Type: scenario.Script, Name: "RETRY"}

	retryIn, err := w.runRetryScript(token, extract, "test.sinq", 1*time.Second)
	if err == nil {
		t.Error("Expected runRetryScript to fail when Lua script returns a string instead of a number")
	}
	if retryIn != -1 {
		t.Errorf("Expected retryIn to be -1 on type failure, got %d", retryIn)
	}
}

func TestWorker_RunRetryScript_NonScriptTokenReturnsNoRetry(t *testing.T) {
	w := setupTestWorker(t, nil)

	token := scenario.Token{Type: scenario.Text}
	extract := func(scenario.Token) []byte { return []byte("unused") }

	retryIn, err := w.runRetryScript(token, extract, "test.sinq", 1*time.Second)
	if err != nil {
		t.Fatalf("expected nil error for a non-script retry token, got %v", err)
	}
	if retryIn != -1 {
		t.Errorf("expected retryIn to be -1 for a non-script retry token, got %d", retryIn)
	}
}

func TestWorker_RunRetryScript_ExecutionError(t *testing.T) {
	w := setupTestWorker(t, nil)

	token := scenario.Token{Type: scenario.Script, Name: "RETRY"}
	extract := func(scenario.Token) []byte { return []byte("if then") }

	retryIn, err := w.runRetryScript(token, extract, "test.sinq", 1*time.Second)
	if err == nil {
		t.Fatal("expected an error from a broken retry script")
	}
	if retryIn != -1 {
		t.Errorf("expected retryIn to be -1 on execution error, got %d", retryIn)
	}
}

func TestWorker_CompileScript_SyntaxError(t *testing.T) {
	w := setupTestWorker(t, nil)

	extract := func(scenario.Token) []byte { return []byte(`if then`) }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	_, err := w.env.compiler.compileScript(token, extract, "test.sinq")
	if err == nil {
		t.Error("Expected syntax error during Lua compilation")
	}
}

type errWriterCloser struct{}

func (errWriterCloser) Write(p []byte) (n int, err error) { return 0, errors.New("write error") }
func (errWriterCloser) MkDirall(dirpath string) error     { return nil }
func (errWriterCloser) Close() error                      { return nil }

type errCreateWorkspace struct{ mockWorkspace }

func (errCreateWorkspace) Create(name string) (io.WriteCloser, error) { return errWriterCloser{}, nil }

func TestWorker_CaptureBodyToFile_Error(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.workspace = &errCreateWorkspace{mockWorkspace{FS: fstest.MapFS{}}}

	_, err := w.captureBodyToFile(bytes.NewReader([]byte("test")), "out.txt")
	if err == nil {
		t.Error("Expected error when file writing fails")
	}
}

func TestWorker_ExecuteAndExtractValue_RuntimeError(t *testing.T) {
	w := setupTestWorker(t, nil)

	script := `local a = undeclared_var + 1`
	extract := func(scenario.Token) []byte { return []byte(script) }
	token := scenario.Token{Type: scenario.Script, Name: "RUNTIME_FAIL"}

	_, err := w.executeAndExtractValue(token, extract, "test.sinq", 1*time.Second)

	if err == nil {
		t.Fatal("Expected runtime error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot perform add operation") {
		t.Errorf("Expected arithmetic error, got: %v", err)
	}
}

func TestWorker_FinishScenario(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.maxRequestIdx = 5
	w.requestIdx = 1

	w.lc.Push(w.lc.NewClosure(w.finishScenario))
	err := w.lc.PCall(0, 0, nil)
	if err != nil {
		t.Fatalf("finishScenario failed: %v", err)
	}

	if w.requestIdx != 6 {
		t.Errorf("Expected requestIdx to be maxRequestIdx + 1, got %d", w.requestIdx)
	}
}

func TestWorker_CompareFiles(t *testing.T) {
	w := setupTestWorker(t, nil)

	mockFS := fstest.MapFS{
		"req.txt":   &fstest.MapFile{Data: []byte("match")},
		"other.txt": &fstest.MapFile{Data: []byte("match")},
		"diff.txt":  &fstest.MapFile{Data: []byte("different")},
	}
	w.env.workspace = &mockWorkspace{FS: mockFS}

	tests := []struct {
		name        string
		saveTo      string
		otherFile   string
		expectError string
	}{
		{"Empty SaveTo", "", "other.txt", "not saved to file"},
		{"Missing SaveTo", "missing.txt", "other.txt", "failed to open saved response"},
		{"Missing OtherFile", "req.txt", "missing.txt", "failed to open file"},
		{"Match", "req.txt", "other.txt", ""},
		{"Diff", "req.txt", "diff.txt", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn := w.compareFiles("script.sinq", tt.saveTo)
			w.lc.Push(w.lc.NewClosure(fn))
			w.lc.Push(lua.LString(tt.otherFile))
			err := w.lc.PCall(1, 0, nil)

			if tt.expectError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectError) {
					t.Errorf("Expected error containing %q, got %v", tt.expectError, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.name == "Diff" {
					if len(w.assertionFailures) == 0 {
						t.Errorf("Expected assertion failure for Diff, got none")
					}
				}
			}
		})
	}
}

func TestWorker_RequestCompleted(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)

	resp := intermediate{
		statusCode: 400,
		headers:    http.Header{"X-Test": []string{"Val"}},
		body:       []byte("body"),
		filenameTo: "saved.txt",
		size:       4,
	}

	w.env.cfg.DumpOnFailure = true
	resStr, err := w.requestCompleted(resp)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if !strings.Contains(resStr, "X-Test: Val") {
		t.Errorf("Expected dump to contain X-Test: Val, got: %s", resStr)
	}
}

type trackMkdirWorkspace struct {
	mockWorkspace
	createdDirs []string
	failMkdir   bool
}

func (t *trackMkdirWorkspace) MkDirall(name string) error {
	if t.failMkdir {
		return errors.New("mkdir failed")
	}
	t.createdDirs = append(t.createdDirs, name)
	return nil
}

type mockDirFileInfo struct{ name string }

func (mockDirFileInfo) Name() string       { return "dir" }
func (mockDirFileInfo) Size() int64        { return 0 }
func (mockDirFileInfo) Mode() fs.FileMode  { return fs.ModeDir }
func (mockDirFileInfo) ModTime() time.Time { return time.Time{} }
func (mockDirFileInfo) IsDir() bool        { return true }
func (mockDirFileInfo) Sys() any           { return nil }

func (t *trackMkdirWorkspace) Stat(name string) (fs.FileInfo, error) {
	if slices.Contains(t.createdDirs, name) {
		return mockDirFileInfo{name: name}, nil
	}
	return t.mockWorkspace.Stat(name)
}

func TestWorker_SaveResponseTo_Mkdir(t *testing.T) {
	w := setupTestWorker(t, nil)

	mockFS := fstest.MapFS{
		"test.sinq": &fstest.MapFile{Data: []byte("request")},
	}
	trackFS := &trackMkdirWorkspace{mockWorkspace: mockWorkspace{FS: mockFS}}
	w.env.workspace = trackFS
	w.lc.SetupRequestEnvironment(0)

	script := `req.saveResponseTo('newdir/out.txt')`
	extract := func(scenario.Token) []byte { return []byte(script) }
	token := scenario.Token{Type: scenario.Script, Name: "PRE"}

	res, err := w.runPreScript(token, extract, "test.sinq", 1*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if res.filenameOut != "newdir/out.txt" {
		t.Errorf("Expected filenameOut to be newdir/out.txt, got %s", res.filenameOut)
	}
	if len(trackFS.createdDirs) != 1 || trackFS.createdDirs[0] != "newdir" {
		t.Errorf("Expected newdir to be created, got %v", trackFS.createdDirs)
	}

	trackFS.failMkdir = true
	scriptFail := `req.saveResponseTo('faildir/out.txt')`
	extractFail := func(scenario.Token) []byte { return []byte(scriptFail) }

	_, errFail := w.runPreScript(token, extractFail, "test.sinq", 1*time.Second)
	if errFail == nil {
		t.Fatal("Expected error on mkdir failure, got nil")
	}
	if !strings.Contains(errFail.Error(), "failed to create directory") {
		t.Errorf("Expected failure message to contain 'failed to create directory', got: %v", errFail)
	}
}

func TestWorker_SetRequestIdx(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.maxRequestIdx = 3

	call := func(n float64) {
		w.lc.Push(w.lc.NewClosure(w.setRequestIdx))
		w.lc.Push(lua.LNumber(n))
		if err := w.lc.PCall(1, 0, nil); err != nil {
			t.Fatalf("PCall failed: %v", err)
		}
	}

	w.requestIdx = 0
	call(2)
	if w.requestIdx != 1 {
		t.Errorf("expected requestIdx 1 for a valid in-range index, got %d", w.requestIdx)
	}

	w.requestIdx = 1
	call(0)
	if w.requestIdx != 1 {
		t.Errorf("expected requestIdx to stay unchanged at 1 for an out-of-range negative index, got %d", w.requestIdx)
	}

	w.requestIdx = 1
	call(100)
	if w.requestIdx != 1 {
		t.Errorf("expected requestIdx to stay unchanged at 1 for an out-of-range index, got %d", w.requestIdx)
	}

	w.requestIdx = 1
	call(4)
	if w.requestIdx != 3 {
		t.Errorf("expected requestIdx 3 at the upper boundary, got %d", w.requestIdx)
	}

	w.requestIdx = 1
	call(1)
	if w.requestIdx != 0 {
		t.Errorf("expected requestIdx 0 at the lower boundary, got %d", w.requestIdx)
	}
}

func TestFixError(t *testing.T) {
	syntaxErr := &lua.ApiError{Type: lua.ApiErrorSyntax}
	if fixError(syntaxErr) != nil {
		t.Error("expected nil for an ApiError of type ApiErrorSyntax")
	}

	runErr := &lua.ApiError{Type: lua.ApiErrorRun}
	if got := fixError(runErr); got != runErr {
		t.Errorf("expected the same error returned unchanged for a non-syntax ApiError, got %v", got)
	}

	pErr := &parse.Error{}
	if fixError(pErr) != nil {
		t.Error("expected nil for a *parse.Error")
	}

	other := errors.New("some other error")
	if got := fixError(other); got != other {
		t.Errorf("expected the same error returned unchanged for an unrelated error type, got %v", got)
	}
}

func TestWorker_CompareFiles_ReadErrors(t *testing.T) {
	w := setupTestWorker(t, nil)

	baseFS := fstest.MapFS{
		"req.txt":   &fstest.MapFile{Data: []byte("match")},
		"other.txt": &fstest.MapFile{Data: []byte("match")},
	}

	t.Run("saved response read error", func(t *testing.T) {
		w.env.workspace = &erroringOpenWorkspace{
			mockWorkspace: mockWorkspace{FS: baseFS},
			target:        "req.txt",
			err:           errors.New("disk read failure"),
		}

		fn := w.compareFiles("script.sinq", "req.txt")
		w.lc.Push(w.lc.NewClosure(fn))
		w.lc.Push(lua.LString("other.txt"))
		err := w.lc.PCall(1, 0, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to read from saved response") {
			t.Errorf("expected a 'failed to read from saved response' error, got %v", err)
		}
	})

	t.Run("comparison file read error", func(t *testing.T) {
		w.env.workspace = &erroringOpenWorkspace{
			mockWorkspace: mockWorkspace{FS: baseFS},
			target:        "other.txt",
			err:           errors.New("disk read failure"),
		}

		fn := w.compareFiles("script.sinq", "req.txt")
		w.lc.Push(w.lc.NewClosure(fn))
		w.lc.Push(lua.LString("other.txt"))
		err := w.lc.PCall(1, 0, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to read from file") {
			t.Errorf("expected a 'failed to read from file' error, got %v", err)
		}
	})
}

type erroringReadFile struct{ err error }

func (erroringReadFile) Stat() (fs.FileInfo, error)   { return nil, nil }
func (e erroringReadFile) Read(p []byte) (int, error) { return 0, e.err }
func (erroringReadFile) Close() error                 { return nil }

type erroringOpenWorkspace struct {
	mockWorkspace
	target string
	err    error
}

func (w *erroringOpenWorkspace) Open(name string) (fs.File, error) {
	if name == w.target {
		return erroringReadFile{err: w.err}, nil
	}
	return w.mockWorkspace.Open(name)
}

func TestWorker_ExecuteAndExtractValue_DiffCalculatedFromCorrectBase(t *testing.T) {
	t.Run("value produced on top of existing stack contents", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		w.lc.Push(lua.LString("junk1"))
		w.lc.Push(lua.LString("junk2"))
		w.lc.Push(lua.LString("junk3"))

		extract := func(scenario.Token) []byte { return []byte(`return "value"`) }
		token := scenario.Token{Type: scenario.Script, Name: "DIFF_BASE_OK"}

		val, err := w.executeAndExtractValue(token, extract, "test.sinq", 1*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.String() != "value" {
			t.Errorf("expected 'value', got %q", val.String())
		}
	})

	t.Run("no value produced on top of existing stack contents", func(t *testing.T) {
		w := setupTestWorker(t, nil)
		w.lc.Push(lua.LString("junk1"))
		w.lc.Push(lua.LString("junk2"))
		w.lc.Push(lua.LString("junk3"))

		extract := func(scenario.Token) []byte { return []byte(`local x = 1`) }
		token := scenario.Token{Type: scenario.Script, Name: "DIFF_BASE_EMPTY"}

		_, err := w.executeAndExtractValue(token, extract, "test.sinq", 1*time.Second)
		if err == nil {
			t.Fatal("expected an error because the script produced no value, but got success")
		}
		if !strings.Contains(err.Error(), "produced no value") {
			t.Errorf("expected a 'produced no value' error, got %v", err)
		}
	})
}

func TestWorker_RequestCompleted_MultiValueHeaderCommaJoin(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.SetupRequestEnvironment(0)
	w.env.cfg.DumpOnFailure = true

	resp := intermediate{
		statusCode: 200,
		status:     "200 OK",
		proto:      "HTTP/1.1",
		headers:    http.Header{"X-Multi": []string{"a", "b", "c"}},
		body:       []byte("body\n"),
	}

	resStr, err := w.requestCompleted(resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resStr, "X-Multi: a, b, c\n") {
		t.Errorf("expected comma-joined header values with no trailing comma, got: %q", resStr)
	}
}

func TestWorker_RequestCompleted_TrailingNewlineHandling(t *testing.T) {
	run := func(t *testing.T, body []byte) string {
		t.Helper()
		w := setupTestWorker(t, nil)
		w.lc.SetupRequestEnvironment(0)
		w.env.cfg.DumpOnFailure = true

		resp := intermediate{
			statusCode: 200,
			status:     "200 OK",
			proto:      "HTTP/1.1",
			headers:    http.Header{},
			body:       body,
		}

		resStr, err := w.requestCompleted(resp)
		if err != nil {
			t.Fatalf("unexpected error (possible out-of-range panic on empty body): %v", err)
		}
		return resStr
	}

	t.Run("already ends with newline", func(t *testing.T) {
		resStr := run(t, []byte("has newline\n"))
		if !strings.HasSuffix(resStr, "has newline\n") {
			t.Errorf("expected result to end with the body, got %q", resStr)
		}
		if strings.HasSuffix(resStr, "has newline\n\n") {
			t.Errorf("expected no extra newline appended when the body already ends with one, got %q", resStr)
		}
	})

	t.Run("no trailing newline", func(t *testing.T) {
		resStr := run(t, []byte("no newline"))
		if !strings.HasSuffix(resStr, "no newline\n") {
			t.Errorf("expected a trailing newline to be appended, got %q", resStr)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		run(t, []byte{})
	})
}
