// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yuin/gopher-lua"
)

func TestExtractBodyJson(t *testing.T) {
	t.Run("Success: Parses valid JSON and caches it", func(t *testing.T) {
		ls := lua.NewState()
		defer ls.Close()

		reqTable := ls.NewTable()
		reqTable.RawSetString("bodyRaw", lua.LString(`{"status": "ok", "count": 42}`))

		closure := ls.NewClosure(ExtractBodyJson, reqTable)
		ls.Push(closure)

		if err := ls.PCall(0, 2, nil); err != nil {
			t.Fatalf("unexpected lua execution error: %v", err)
		}

		errVal := ls.Get(-1)
		resVal := ls.Get(-2)

		if errVal != lua.LNil {
			t.Errorf("expected error to be nil, got %v", errVal)
		}
		if resVal.Type() != lua.LTTable {
			t.Errorf("expected result to be a table, got %v", resVal.Type())
		}

		cached := reqTable.RawGetString("bodyJson")
		if cached == lua.LNil {
			t.Errorf("expected bodyJson to be cached on the table")
		}
	})

	t.Run("Success: Returns cached value immediately (Type Agnostic)", func(t *testing.T) {
		ls := lua.NewState()
		defer ls.Close()

		reqTable := ls.NewTable()
		reqTable.RawSetString("bodyJson", lua.LBool(true))
		reqTable.RawSetString("bodyRaw", lua.LString(`{bad_json}`))

		closure := ls.NewClosure(ExtractBodyJson, reqTable)
		ls.Push(closure)

		if err := ls.PCall(0, 2, nil); err != nil {
			t.Fatalf("unexpected lua execution error: %v", err)
		}

		errVal := ls.Get(-1)
		resVal := ls.Get(-2)

		if errVal != lua.LNil {
			t.Errorf("expected error to be nil, got %v", errVal)
		}
		if resVal.Type() != lua.LTBool || resVal != lua.LBool(true) {
			t.Errorf("expected result to be cached true, got %v", resVal)
		}
	})

	t.Run("Failure: Invalid JSON returns nil and error", func(t *testing.T) {
		ls := lua.NewState()
		defer ls.Close()

		reqTable := ls.NewTable()
		reqTable.RawSetString("bodyRaw", lua.LString(`{"status": "incomplete"`))

		closure := ls.NewClosure(ExtractBodyJson, reqTable)
		ls.Push(closure)

		if err := ls.PCall(0, 2, nil); err != nil {
			t.Fatalf("unexpected lua execution error: %v", err)
		}

		errVal := ls.Get(-1)
		resVal := ls.Get(-2)

		if resVal != lua.LNil {
			t.Errorf("expected result to be nil on failure, got %v", resVal)
		}
		if errVal.Type() != lua.LTString {
			t.Fatalf("expected error to be a string, got %v", errVal.Type())
		}
		if !strings.Contains(errVal.String(), "unexpected end of JSON input") {
			t.Errorf("expected JSON syntax error, got %s", errVal.String())
		}
	})

	t.Run("Failure: Missing bodyRaw", func(t *testing.T) {
		ls := lua.NewState()
		defer ls.Close()

		reqTable := ls.NewTable()

		closure := ls.NewClosure(ExtractBodyJson, reqTable)
		ls.Push(closure)

		if err := ls.PCall(0, 2, nil); err != nil {
			t.Fatalf("unexpected lua execution error: %v", err)
		}

		errVal := ls.Get(-1)
		resVal := ls.Get(-2)

		if resVal != lua.LNil {
			t.Errorf("expected result to be nil, got %v", resVal)
		}
		if errVal.Type() != lua.LTString || !strings.Contains(errVal.String(), "bodyRaw not found") {
			t.Errorf("expected bodyRaw missing error, got %v", errVal)
		}
	})

	t.Run("Failure: Upvalue is not a table", func(t *testing.T) {
		ls := lua.NewState()
		defer ls.Close()

		closure := ls.NewClosure(ExtractBodyJson, lua.LString("I am a teapot"))
		ls.Push(closure)

		if err := ls.PCall(0, 2, nil); err != nil {
			t.Fatalf("unexpected lua execution error: %v", err)
		}

		errVal := ls.Get(-1)
		resVal := ls.Get(-2)

		if resVal != lua.LNil {
			t.Errorf("expected result to be nil, got %v", resVal)
		}
		if errVal.Type() != lua.LTString || !strings.Contains(errVal.String(), "no request table found") {
			t.Errorf("expected upvalue error, got %v", errVal)
		}
	})
}

func TestToLuaValue(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()

	tests := []struct {
		name  string
		input any
		check func(*testing.T, lua.LValue)
	}{
		{"Nil", nil, func(t *testing.T, val lua.LValue) {
			if val != lua.LNil {
				t.Errorf("expected LNil, got %v", val.Type())
			}
		}},
		{"Bool", true, func(t *testing.T, val lua.LValue) {
			if val != lua.LBool(true) {
				t.Errorf("expected LBool(true), got %v", val)
			}
		}},
		{"Float64", 42.5, func(t *testing.T, val lua.LValue) {
			if val != lua.LNumber(42.5) {
				t.Errorf("expected LNumber(42.5), got %v", val)
			}
		}},
		{"Int", int(42), func(t *testing.T, val lua.LValue) {
			if val != lua.LNumber(42) {
				t.Errorf("expected LNumber(42), got %v", val)
			}
		}},
		{"Int64", int64(42), func(t *testing.T, val lua.LValue) {
			if val != lua.LNumber(42) {
				t.Errorf("expected LNumber(42), got %v", val)
			}
		}},
		{"String", "hello", func(t *testing.T, val lua.LValue) {
			if val != lua.LString("hello") {
				t.Errorf("expected LString('hello'), got %v", val)
			}
		}},
		{"Slice", []any{"a", 1}, func(t *testing.T, val lua.LValue) {
			tbl, ok := val.(*lua.LTable)
			if !ok {
				t.Fatalf("expected LTable, got %v", val.Type())
			}
			if tbl.Len() != 2 {
				t.Errorf("expected table length 2, got %d", tbl.Len())
			}
			if tbl.RawGetInt(1) != lua.LString("a") || tbl.RawGetInt(2) != lua.LNumber(1) {
				t.Errorf("slice contents mismatched")
			}
		}},
		{"Map", map[string]any{"key": "value"}, func(t *testing.T, val lua.LValue) {
			tbl, ok := val.(*lua.LTable)
			if !ok {
				t.Fatalf("expected LTable, got %v", val.Type())
			}
			if tbl.RawGetString("key") != lua.LString("value") {
				t.Errorf("map contents mismatched")
			}
		}},
		{"HTTP Header - Single", http.Header{"X-Trace": {"123"}}, func(t *testing.T, val lua.LValue) {
			tbl, ok := val.(*lua.LTable)
			if !ok {
				t.Fatalf("expected LTable, got %v", val.Type())
			}
			if tbl.RawGetString("X-Trace") != lua.LString("123") {
				t.Errorf("header string mismatched")
			}
		}},
		{"HTTP Header - Multiple", http.Header{"Accept": {"text/html", "application/json"}}, func(t *testing.T, val lua.LValue) {
			tbl, ok := val.(*lua.LTable)
			if !ok {
				t.Fatalf("expected LTable, got %v", val.Type())
			}
			arr, ok := tbl.RawGetString("Accept").(*lua.LTable)
			if !ok || arr.Len() != 2 {
				t.Errorf("expected inner array of length 2")
			}
		}},
		{"HTTP Header - Empty", http.Header{"Empty": {}}, func(t *testing.T, val lua.LValue) {
			tbl, ok := val.(*lua.LTable)
			if !ok {
				t.Fatalf("expected LTable, got %v", val.Type())
			}
			if tbl.RawGetString("Empty") != lua.LString("") {
				t.Errorf("expected empty string for missing header values")
			}
		}},
		{"Unknown Type", struct{ A int }{1}, func(t *testing.T, val lua.LValue) {
			if val != lua.LNil {
				t.Errorf("expected LNil for unknown struct, got %v", val.Type())
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ToLuaValue(tt.input, ls)
			tt.check(t, res)
		})
	}
}

func TestExtractBodyJsonUnsafe(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()

	t.Run("Success", func(t *testing.T) {
		reqTable := ls.NewTable()
		reqTable.RawSetString("bodyRaw", lua.LString(`{"status": "ok"}`))

		closure := ls.NewClosure(ExtractBodyJsonUnsafe, reqTable)
		ls.Push(closure)

		if err := ls.PCall(0, 1, nil); err != nil {
			t.Fatalf("unexpected lua execution error: %v", err)
		}

		resVal := ls.Get(-1)
		if resVal.Type() != lua.LTTable {
			t.Errorf("expected result to be a table, got %v", resVal.Type())
		}
	})

	t.Run("Failure throws error string", func(t *testing.T) {
		reqTable := ls.NewTable()
		reqTable.RawSetString("bodyRaw", lua.LString(`{bad_json`))

		closure := ls.NewClosure(ExtractBodyJsonUnsafe, reqTable)
		ls.Push(closure)

		err := ls.PCall(0, 1, nil)
		if err == nil {
			t.Fatalf("expected lua error on unsafe extraction")
		}
	})
}
