// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"

	"github.com/Veitangie/sinq/internal/timer"
	lua "github.com/yuin/gopher-lua"
)

func TestJSONParser_Parse(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
	defer lc.Close()

	p := &lc.parser

	tests := []struct {
		name      string
		input     string
		wantError bool
		assert    func(t *testing.T, val lua.LValue)
	}{
		// --- Primitives ---
		{
			name:      "String literal",
			input:     `"hello world"`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				if val.Type() != lua.LTString || val.String() != "hello world" {
					t.Errorf("expected string 'hello world', got %v (%v)", val, val.Type())
				}
			},
		},
		{
			name:      "Number literal",
			input:     `42.5`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				if val.Type() != lua.LTNumber || float64(val.(lua.LNumber)) != 42.5 {
					t.Errorf("expected number 42.5, got %v (%v)", val, val.Type())
				}
			},
		},
		{
			name:      "Boolean literal",
			input:     `true`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				if val.Type() != lua.LTBool || bool(val.(lua.LBool)) != true {
					t.Errorf("expected bool true, got %v (%v)", val, val.Type())
				}
			},
		},
		{
			name:      "Null literal",
			input:     `null`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				if ud, ok := val.(*lua.LUserData); !ok || !isJSONNull(ud) {
					t.Errorf("expected json.null, got %v (%v)", val, val.Type())
				}
			},
		},

		// --- Objects ---
		{
			name:      "Empty object",
			input:     `{}`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl, ok := val.(*lua.LTable)
				if !ok {
					t.Fatalf("expected table, got %v", val.Type())
				}
				if tbl.Len() != 0 {
					t.Errorf("expected empty table, got length %d", tbl.Len())
				}
			},
		},
		{
			name:      "Simple object",
			input:     `{"name": "sinq", "version": 1}`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl, ok := val.(*lua.LTable)
				if !ok {
					t.Fatalf("expected table, got %v", val.Type())
				}

				name := tbl.RawGetString("name")
				if name.String() != "sinq" {
					t.Errorf("expected name='sinq', got %v", name)
				}

				version := tbl.RawGetString("version")
				if float64(version.(lua.LNumber)) != 1 {
					t.Errorf("expected version=1, got %v", version)
				}
			},
		},

		// --- Arrays ---
		{
			name:      "Empty array",
			input:     `[]`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl, ok := val.(*lua.LTable)
				if !ok {
					t.Fatalf("expected table, got %v", val.Type())
				}
				if tbl.Len() != 0 {
					t.Errorf("expected empty array, got length %d", tbl.Len())
				}
			},
		},
		{
			name:      "Simple array",
			input:     `["a", "b", "c"]`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl, ok := val.(*lua.LTable)
				if !ok {
					t.Fatalf("expected table, got %v", val.Type())
				}

				if tbl.Len() != 3 {
					t.Errorf("expected array length 3, got %d", tbl.Len())
				}
				if tbl.RawGetInt(1).String() != "a" {
					t.Errorf("expected index 1 to be 'a', got %v", tbl.RawGetInt(1))
				}
				if tbl.RawGetInt(3).String() != "c" {
					t.Errorf("expected index 3 to be 'c', got %v", tbl.RawGetInt(3))
				}
			},
		},

		// --- Nested Structures ---
		{
			name:      "Complex nested structure",
			input:     `{"data": [{"id": 10}, {"id": 20}], "active": true}`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				root, ok := val.(*lua.LTable)
				if !ok {
					t.Fatalf("expected table, got %v", val.Type())
				}

				if root.RawGetString("active") != lua.LBool(true) {
					t.Errorf("expected active=true")
				}

				data := root.RawGetString("data").(*lua.LTable)
				if data.Len() != 2 {
					t.Fatalf("expected data array length 2, got %d", data.Len())
				}

				firstItem := data.RawGetInt(1).(*lua.LTable)
				if float64(firstItem.RawGetString("id").(lua.LNumber)) != 10 {
					t.Errorf("expected data[1].id = 10, got %v", firstItem.RawGetString("id"))
				}
			},
		},

		// --- Error Cases ---
		{
			name:      "Malformed JSON (trailing comma)",
			input:     `{"a": 1,}`,
			wantError: true,
			assert:    nil,
		},
		{
			name:      "Malformed JSON (missing value)",
			input:     `{"a": }`,
			wantError: true,
			assert:    nil,
		},
		{
			name:      "Empty input",
			input:     ``,
			wantError: true,
			assert:    nil,
		},
		// --- Edge Cases ---
		{
			name:      "The LNil Trap (Null in Object)",
			input:     `{"valid": 1, "missing": null}`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl := val.(*lua.LTable)

				ud, ok := tbl.RawGetString("missing").(*lua.LUserData)
				if !ok || !isJSONNull(ud) {
					t.Errorf("expected missing key to be json.null, got %v", tbl.RawGetString("missing"))
				}
				if float64(tbl.RawGetString("valid").(lua.LNumber)) != 1 {
					t.Errorf("expected valid=1")
				}
			},
		},
		{
			name:      "The LNil Trap (Null in Array)",
			input:     `[1, null, 3]`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl := val.(*lua.LTable)

				if tbl.RawGetInt(1) != lua.LNumber(1) {
					t.Errorf("index 1 expected 1")
				}
				ud, ok := tbl.RawGetInt(2).(*lua.LUserData)
				if !ok || !isJSONNull(ud) {
					t.Errorf("index 2 expected json.null")
				}
				if tbl.RawGetInt(3) != lua.LNumber(3) {
					t.Errorf("index 3 expected 3")
				}
			},
		},
		{
			name:      "Heterogeneous (Mixed) Array",
			input:     `[42, "hello", true, {"a": 1}]`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl := val.(*lua.LTable)
				if tbl.Len() != 4 {
					t.Fatalf("expected length 4, got %d", tbl.Len())
				}
				if tbl.RawGetInt(1) != lua.LNumber(42) {
					t.Errorf("expected index 1 = 42")
				}
				if tbl.RawGetInt(2) != lua.LString("hello") {
					t.Errorf("expected index 2 = 'hello'")
				}
				if tbl.RawGetInt(3) != lua.LBool(true) {
					t.Errorf("expected index 3 = true")
				}

				obj := tbl.RawGetInt(4).(*lua.LTable)
				if obj.RawGetString("a") != lua.LNumber(1) {
					t.Errorf("expected index 4 to contain {a: 1}")
				}
			},
		},
		{
			name:      "Scientific Notation & Negatives",
			input:     `[-42.5, 1.5e10]`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl := val.(*lua.LTable)
				if float64(tbl.RawGetInt(1).(lua.LNumber)) != -42.5 {
					t.Errorf("expected -42.5")
				}
				if float64(tbl.RawGetInt(2).(lua.LNumber)) != 1.5e10 {
					t.Errorf("expected 1.5e10")
				}
			},
		},
		{
			name:      "Escaped Strings and Unicode",
			input:     `{"text": "hello\n\"world\"", "emoji": "🚀"}`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				tbl := val.(*lua.LTable)
				if tbl.RawGetString("text").String() != "hello\n\"world\"" {
					t.Errorf("escape sequences failed")
				}
				if tbl.RawGetString("emoji").String() != "🚀" {
					t.Errorf("unicode failed")
				}
			},
		},
		{
			name:      "Deeply Nested Arrays",
			input:     `[[[1]]]`,
			wantError: false,
			assert: func(t *testing.T, val lua.LValue) {
				l1 := val.(*lua.LTable)
				l2 := l1.RawGetInt(1).(*lua.LTable)
				l3 := l2.RawGetInt(1).(*lua.LTable)
				if l3.RawGetInt(1) != lua.LNumber(1) {
					t.Errorf("deep recursion failed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.Parse(tt.input)

			if (err != nil) != tt.wantError {
				t.Fatalf("Parse() error = %v, wantError %v", err, tt.wantError)
			}

			if !tt.wantError && tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestJSONSerializer_SerializeLValue(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
	defer lc.Close()
	ls := &lc.LState
	mapToTable := func(m map[string]lua.LValue) *lua.LTable {
		tbl := ls.NewTable()
		for k, v := range m {
			tbl.RawSetString(k, v)
		}
		return tbl
	}
	sliceToTable := func(s []lua.LValue) *lua.LTable {
		tbl := ls.NewTable()
		for i, v := range s {
			tbl.RawSetInt(i+1, v)
		}
		return tbl
	}
	emptyArrayTable := func() *lua.LTable {
		tbl := ls.NewTable()
		mt := ls.NewTable()
		mt.RawSetString("asEmptyArray", lua.LBool(true))
		tbl.Metatable = mt
		return tbl
	}
	mixedTable := func() *lua.LTable {
		tbl := ls.NewTable()
		tbl.RawSetInt(1, lua.LNumber(100))
		tbl.RawSetString("key", lua.LString("value"))
		return tbl
	}
	tests := []struct {
		name      string
		val       lua.LValue
		indent    string
		newline   bool
		want      string
		wantError bool
	}{
		// --- Cycle & DAG ---
		{
			name: "Cycle Detection",
			val: func() lua.LValue {
				tbl := ls.NewTable()
				tbl.RawSetString("self", tbl)
				return tbl
			}(),
			wantError: true,
		},
		{
			name: "DAG Support (No false cycles)",
			val: func() lua.LValue {
				child := ls.NewTable()
				child.RawSetString("key", lua.LString("val"))
				parent := ls.NewTable()
				parent.RawSetInt(1, child)
				parent.RawSetInt(2, child)
				return parent
			}(),
			want:      `[{"key":"val"},{"key":"val"}]`,
			wantError: false,
		},
		// --- Primitives (Compact) ---
		{
			name:      "String with quotes to escape",
			val:       lua.LString(`hello "world"`),
			want:      `"hello \"world\""`,
			wantError: false,
		},
		{
			name:      "Number",
			val:       lua.LNumber(42.5),
			want:      `42.5`,
			wantError: false,
		},
		{
			name:      "Boolean",
			val:       lua.LBool(true),
			want:      `true`,
			wantError: false,
		},
		{
			name:      "Nil",
			val:       lc.parser.LNull,
			want:      `null`,
			wantError: false,
		},
		// --- Empty Tables ---
		{
			name:      "Empty Table (Defaults to Object)",
			val:       ls.NewTable(),
			want:      `{}`,
			wantError: false,
		},
		{
			name:      "Empty Table with asEmptyArray = true",
			val:       emptyArrayTable(),
			want:      `[]`,
			wantError: false,
		},
		// --- Flat Structures (Compact) ---
		{
			name:      "Simple Array",
			val:       sliceToTable([]lua.LValue{lua.LNumber(1), lua.LString("two")}),
			want:      `[1,"two"]`,
			wantError: false,
		},
		{
			name:      "Simple Object (Alphabetical Key Sorting)",
			val:       mapToTable(map[string]lua.LValue{"z": lua.LNumber(1), "a": lua.LNumber(2)}),
			want:      `{"a":2,"z":1}`,
			wantError: false,
		},
		{
			name:      "Mixed Table (Int and String keys)",
			val:       mixedTable(),
			wantError: true,
		},
		{
			name: "Key escaping",
			val: mapToTable(map[string]lua.LValue{
				`hello "world"`: lua.LNumber(1),
				"line\nbreak":   lua.LNumber(2),
			}),
			want:      `{"hello \"world\"":1,"line\nbreak":2}`,
			wantError: false,
		},
		// --- Pretty Printing (newline = true) ---
		{
			name:    "Pretty Print Simple Object (2 spaces)",
			val:     mapToTable(map[string]lua.LValue{"b": lua.LBool(true), "a": lua.LNumber(1)}),
			indent:  "  ",
			newline: true,
			want: `{
  "a": 1,
  "b": true
}`,
			wantError: false,
		},
		{
			name:    "Pretty Print Simple Array (tabs)",
			val:     sliceToTable([]lua.LValue{lua.LNumber(1), lua.LNumber(2)}),
			indent:  "\t",
			newline: true,
			want: `[
	1,
	2
]`,
			wantError: false,
		},
		{
			name: "Pretty Print Nested Structure",
			val: mapToTable(map[string]lua.LValue{
				"arr": sliceToTable([]lua.LValue{lua.LNumber(10)}),
			}),
			indent:  "  ",
			newline: true,
			want: `{
  "arr": [
    10
  ]
}`,
			wantError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &JSONSerializer{
				indent:  tt.indent,
				newline: tt.newline,
			}
			got, err := s.SerializeLValue(tt.val)
			if (err != nil) != tt.wantError {
				t.Fatalf("SerializeLValue() error = %v, wantError %v", err, tt.wantError)
			}
			if !tt.wantError && got != tt.want {
				t.Errorf("SerializeLValue() \nGot:  %s\nWant: %s", got, tt.want)
			}
		})
	}
}

func TestSerializeToJSON(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
	defer lc.Close()
	ls := &lc.LState

	t.Run("Success", func(t *testing.T) {
		ls.Push(ls.NewFunction(lc.SerializeToJSON))
		tbl := ls.NewTable()
		tbl.RawSetString("key", lua.LString("value"))
		ls.Push(tbl)
		ls.Push(lua.LString(""))

		if err := ls.PCall(2, 2, nil); err != nil {
			t.Fatalf("unexpected lua error: %v", err)
		}

		errRes := ls.Get(-1)
		valRes := ls.Get(-2)
		ls.Pop(2)

		if errRes != lua.LNil {
			t.Fatalf("expected nil error, got %v", errRes)
		}

		if valRes.String() != `{"key":"value"}` {
			t.Errorf("unexpected json output: %s", valRes.String())
		}
	})

	t.Run("Failure", func(t *testing.T) {
		ls.Push(ls.NewFunction(lc.SerializeToJSON))
		tbl := ls.NewTable()
		tbl.RawSetInt(1, lua.LNumber(1))
		tbl.RawSetString("key", lua.LString("value"))
		ls.Push(tbl)

		if err := ls.PCall(1, 2, nil); err != nil {
			t.Fatalf("unexpected lua error: %v", err)
		}

		errRes := ls.Get(-1)
		valRes := ls.Get(-2)
		ls.Pop(2)

		if errRes == lua.LNil {
			t.Fatalf("expected error, got nil")
		}
		if valRes != lua.LNil {
			t.Errorf("expected value to be nil on error, got %v", valRes)
		}
	})
}
