// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
)

func TestWorker_ToLuaValue(t *testing.T) {
	w := &worker{
		scriptCacheLock: &sync.RWMutex{},
		scriptCache:     make(map[scriptKey]*lua.FunctionProto),
		wg:              &sync.WaitGroup{},
	}
	err := w.setupLuaEnvironment(context.Background(), nil)
	if err != nil {
		t.Fatalf("Failed to setup Lua environment: %v", err)
	}
	w.wg.Add(1)
	defer w.Close()

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
			got := w.toLuaValue(tt.input)
			tt.validate(t, got)
		})
	}
}

func TestWorker_ExecuteAndExtractValue_ImplicitReturn(t *testing.T) {
	w := &worker{
		scriptCacheLock: &sync.RWMutex{},
		scriptCache:     make(map[scriptKey]*lua.FunctionProto),
		wg:              &sync.WaitGroup{},
	}
	err := w.setupLuaEnvironment(context.Background(), nil)
	if err != nil {
		t.Fatalf("Failed to setup Lua environment: %v", err)
	}
	w.wg.Add(1)
	defer w.Close()

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
