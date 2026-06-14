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
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Veitangie/sinq/internal/luapi"
	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
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
			got := luapi.ToLuaValue(tt.input, &w.lc.LState)
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

func TestWorker_GiantPayload_HandledSafely(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.lc.setupRequestEnvironment(0)

	garbage := bytes.Repeat([]byte("A"), 10*1024*1024)
	resp := &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(garbage)),
	}

	err := w.requestCompleted(context.Background(), resp, "", 1<<10, 1)
	if err != nil {
		t.Fatalf("requestCompleted failed unexpectedly: %v", err)
	}

	err = w.lc.DoString(`
		local req = sinq.responses[1]
		if req == nil then error("response missing") end
		
		if req.oversized ~= true then
			error("Engine survived, but failed to flag the giant payload as 'oversized' in the Lua table!")
		end
	`)
	if err != nil {
		t.Fatalf("Giant payload assertion failed: %v", err)
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

	_, err := w.runRetryScript(token, extract, "test.sinq", 1*time.Second)
	if err == nil {
		t.Error("Expected runRetryScript to fail when Lua script returns a string instead of a number")
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
func (errWriterCloser) Close() error                      { return nil }

type errCreateWorkspace struct{ mockWorkspace }

func (errCreateWorkspace) Create(name string) (io.WriteCloser, error) { return errWriterCloser{}, nil }

func TestWorker_CaptureBodyToFile_Error(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.workspace = &errCreateWorkspace{mockWorkspace{FS: fstest.MapFS{}}}

	err := w.captureBodyToFile(bytes.NewReader([]byte("test")), "out.txt")
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
