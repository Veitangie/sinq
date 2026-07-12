// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

type testHarness struct {
	ls       *lua.LState
	failures []string
}

func setupHarness() *testHarness {
	ls := lua.NewState()
	ls.OpenLibs()

	h := &testHarness{
		ls:       ls,
		failures: make([]string, 0),
	}

	sinqTbl := ls.NewTable()
	assertTbl := ls.NewTable()
	assertTbl.RawSetString("fail", ls.NewFunction(func(L *lua.LState) int {
		msg := L.CheckString(1)
		h.failures = append(h.failures, msg)
		return 0
	}))
	sinqTbl.RawSetString("assert", assertTbl)

	retryTbl := ls.NewTable()
	retryTbl.RawSetString("when", ls.NewFunctionFromProto(RetryWhen()))
	retryTbl.RawSetString("exponential", ls.NewFunctionFromProto(RetryExponential()))
	sinqTbl.RawSetString("retry", retryTbl)

	ls.SetGlobal("sinq", sinqTbl)

	resTbl := ls.NewTable()
	ls.SetGlobal("res", resTbl)

	return h
}

func TestLuaAssertions(t *testing.T) {
	tests := []struct {
		name          string
		protoFunc     func() *lua.FunctionProto
		setupRes      func(*lua.LState, *lua.LTable)
		args          []any
		wantFailures  int
		failureSubstr string
		expectLuaErr  bool
	}{
		// --- Assert Code ---
		{
			name:      "AssertCode: Success",
			protoFunc: AssertCode,
			setupRes:  func(ls *lua.LState, res *lua.LTable) { res.RawSetString("code", lua.LNumber(200)) },
			args:      []any{200},
		},
		{
			name:          "AssertCode: Failure (Default Message)",
			protoFunc:     AssertCode,
			setupRes:      func(ls *lua.LState, res *lua.LTable) { res.RawSetString("code", lua.LNumber(404)) },
			args:          []any{200},
			wantFailures:  1,
			failureSubstr: "Expected code 200, got 404",
		},
		{
			name:          "AssertCode: Failure (Custom Message)",
			protoFunc:     AssertCode,
			setupRes:      func(ls *lua.LState, res *lua.LTable) { res.RawSetString("code", lua.LNumber(500)) },
			args:          []any{200, "API completely down"},
			wantFailures:  1,
			failureSubstr: "API completely down",
		},

		// --- Assert Condition ---
		{
			name:      "AssertCondition: Success",
			protoFunc: AssertCondition,
			args:      []any{true, "Should not fail"},
		},
		{
			name:          "AssertCondition: Failure",
			protoFunc:     AssertCondition,
			args:          []any{false, "Custom math failed"},
			wantFailures:  1,
			failureSubstr: "Custom math failed",
		},
		{
			name:          "AssertCondition: Panic on missing message",
			protoFunc:     AssertCondition,
			args:          []any{false},
			wantFailures:  1,
			failureSubstr: "sinq.assert.isTrue: Assertion failed",
		},

		// --- Assert Contains ---
		{
			name:      "AssertContains: Success",
			protoFunc: AssertContains,
			args:      []any{"The quick brown fox", "brown fox"},
		},
		{
			name:          "AssertContains: Failure",
			protoFunc:     AssertContains,
			args:          []any{"The quick brown fox", "lazy dog"},
			wantFailures:  1,
			failureSubstr: "did not contain lazy dog",
		},

		// --- Assert Equals ---
		{
			name:      "AssertEquals: Scalar Match",
			protoFunc: AssertEquals,
			args:      []any{"admin", "admin"},
		},
		{
			name:          "AssertEquals: Scalar Mismatch",
			protoFunc:     AssertEquals,
			args:          []any{"admin", "user"},
			wantFailures:  1,
			failureSubstr: "Expected user, got admin instead",
		},
		{
			name:          "AssertEquals: Type Mismatch",
			protoFunc:     AssertEquals,
			args:          []any{42, "42"},
			wantFailures:  1,
			failureSubstr: "Expected type string, got type number instead",
		},
		{
			name:      "AssertEquals: Table Subset Match (Success)",
			protoFunc: AssertEquals,
			args: []any{
				map[string]any{"id": 1, "status": "active", "ts": 999},
				map[string]any{"status": "active"},
			},
		},
		{
			name:      "AssertEquals: Deep Table Subset Match (Success)",
			protoFunc: AssertEquals,
			args: []any{
				map[string]any{"user": map[string]any{"role": "admin", "age": 30}},
				map[string]any{"user": map[string]any{"role": "admin"}},
			},
		},
		{
			name:          "AssertEquals: Table Value Mismatch",
			protoFunc:     AssertEquals,
			args:          []any{map[string]any{"status": "pending"}, map[string]any{"status": "active"}},
			wantFailures:  1,
			failureSubstr: "Expected value active, got pending in field status",
		},
		{
			name:          "AssertEquals: Table Missing Key (Nil check)",
			protoFunc:     AssertEquals,
			args:          []any{map[string]any{"id": 1}, map[string]any{"status": "active"}},
			wantFailures:  1,
			failureSubstr: "Expected type string, got type nil in field status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := setupHarness()
			defer h.ls.Close()

			if tt.setupRes != nil {
				resTbl := h.ls.GetGlobal("res").(*lua.LTable)
				tt.setupRes(h.ls, resTbl)
			}

			h.ls.Push(h.ls.NewFunctionFromProto(tt.protoFunc()))
			for _, arg := range tt.args {
				h.ls.Push(ToLuaValue(arg, h.ls))
			}

			err := h.ls.PCall(len(tt.args), 0, nil)

			if tt.expectLuaErr {
				if err == nil {
					t.Fatalf("Expected hard Lua runtime error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Unexpected Lua error: %v", err)
			}

			if len(h.failures) != tt.wantFailures {
				t.Errorf("Expected %d failures, got %d. Failures: %v", tt.wantFailures, len(h.failures), h.failures)
			}

			if tt.wantFailures > 0 && tt.failureSubstr != "" {
				if !strings.Contains(h.failures[0], tt.failureSubstr) {
					t.Errorf("Expected failure message to contain %q, got %q", tt.failureSubstr, h.failures[0])
				}
			}
		})
	}
}

func TestLuaRetries(t *testing.T) {
	tests := []struct {
		name         string
		protoFunc    func() *lua.FunctionProto
		setupRes     func(*lua.LState, *lua.LTable)
		args         []any
		wantResult   float64
		expectLuaErr bool
	}{
		// --- Retry When ---
		{
			name:       "RetryWhen: Condition Met (Default Delay)",
			protoFunc:  RetryWhen,
			args:       []any{true},
			wantResult: 500,
		},
		{
			name:       "RetryWhen: Condition Met (Custom Delay)",
			protoFunc:  RetryWhen,
			args:       []any{true, 1500},
			wantResult: 1500,
		},
		{
			name:       "RetryWhen: Condition Fails",
			protoFunc:  RetryWhen,
			args:       []any{false, 1500},
			wantResult: -1,
		},

		// --- Retry Exponential ---
		{
			name:       "RetryExponential: Attempt 1 (2^1 * 500 = 1000)",
			protoFunc:  RetryExponential,
			setupRes:   func(ls *lua.LState, res *lua.LTable) { res.RawSetString("attempt", lua.LNumber(1)) },
			args:       []any{true},
			wantResult: 1000,
		},
		{
			name:       "RetryExponential: Attempt 3 (2^3 * 100 = 800)",
			protoFunc:  RetryExponential,
			setupRes:   func(ls *lua.LState, res *lua.LTable) { res.RawSetString("attempt", lua.LNumber(3)) },
			args:       []any{true, 2, 100},
			wantResult: 800,
		},
		{
			name:       "RetryExponential: Condition Fails",
			protoFunc:  RetryExponential,
			setupRes:   func(ls *lua.LState, res *lua.LTable) { res.RawSetString("attempt", lua.LNumber(2)) },
			args:       []any{false},
			wantResult: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := setupHarness()
			defer h.ls.Close()

			if tt.setupRes != nil {
				resTbl := h.ls.GetGlobal("res").(*lua.LTable)
				tt.setupRes(h.ls, resTbl)
			}

			h.ls.Push(h.ls.NewFunctionFromProto(tt.protoFunc()))
			for _, arg := range tt.args {
				h.ls.Push(ToLuaValue(arg, h.ls))
			}

			err := h.ls.PCall(len(tt.args), 1, nil)
			if err != nil {
				t.Fatalf("Unexpected Lua error: %v", err)
			}

			resVal := h.ls.Get(-1)
			if resVal.Type() != lua.LTNumber {
				t.Fatalf("Expected numeric return value, got %v", resVal.Type())
			}

			if float64(resVal.(lua.LNumber)) != tt.wantResult {
				t.Errorf("Expected %f, got %f", tt.wantResult, float64(resVal.(lua.LNumber)))
			}
		})
	}
}

func TestLuaRetryJitterBounds(t *testing.T) {
	h := setupHarness()
	defer h.ls.Close()

	resTbl := h.ls.GetGlobal("res").(*lua.LTable)
	resTbl.RawSetString("attempt", lua.LNumber(2))

	jitterProto := RetryJitter()
	expoProto := RetryExponential()

	baseValue := 400.0
	jitterRange := 50.0

	for i := range 100 {
		h.ls.Push(h.ls.NewFunctionFromProto(jitterProto))

		h.ls.Push(lua.LBool(true))
		h.ls.Push(lua.LNumber(jitterRange))
		h.ls.Push(h.ls.NewFunctionFromProto(expoProto))

		h.ls.Push(lua.LNumber(2))
		h.ls.Push(lua.LNumber(100))

		err := h.ls.PCall(5, 1, nil)
		if err != nil {
			t.Fatalf("Jitter execution failed on iteration %d: %v", i, err)
		}

		resVal := h.ls.Get(-1)
		h.ls.Pop(1)

		if resVal.Type() != lua.LTNumber {
			t.Fatalf("Expected numeric return, got %s", resVal.Type().String())
		}

		calc := float64(resVal.(lua.LNumber))

		if calc < (baseValue-jitterRange) || calc > (baseValue+jitterRange) {
			t.Fatalf("Jitter bound broken. Expected value between %.0f and %.0f, got %.0f",
				baseValue-jitterRange, baseValue+jitterRange, calc)
		}
	}
}

func TestLuaRetryJitterConditionFails(t *testing.T) {
	h := setupHarness()
	defer h.ls.Close()

	h.ls.Push(h.ls.NewFunctionFromProto(RetryJitter()))
	h.ls.Push(lua.LBool(false))
	h.ls.Push(lua.LNumber(50))

	err := h.ls.PCall(2, 1, nil)
	if err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	resVal := h.ls.Get(-1)
	if resVal.Type() != lua.LTNumber || float64(resVal.(lua.LNumber)) != -1 {
		t.Fatalf("Expected false condition to bypass jitter and return -1, got %v", resVal)
	}
}
