// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"encoding/json"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/timer"
)

func TestJSONParser_Parse(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
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
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	mapToTable := func(m map[string]lua.LValue) *lua.LTable {
		tbl := lc.NewTable()
		for k, v := range m {
			tbl.RawSetString(k, v)
		}
		return tbl
	}
	sliceToTable := func(s []lua.LValue) *lua.LTable {
		tbl := lc.NewTable()
		for i, v := range s {
			tbl.RawSetInt(i+1, v)
		}
		return tbl
	}
	emptyArrayTable := func() *lua.LTable {
		tbl := lc.NewTable()
		mt := lc.NewTable()
		mt.RawSetString("asEmptyArray", lua.LBool(true))
		tbl.Metatable = mt
		return tbl
	}
	mixedTable := func() *lua.LTable {
		tbl := lc.NewTable()
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
				tbl := lc.NewTable()
				tbl.RawSetString("self", tbl)
				return tbl
			}(),
			wantError: true,
		},
		{
			name: "DAG Support (No false cycles)",
			val: func() lua.LValue {
				child := lc.NewTable()
				child.RawSetString("key", lua.LString("val"))
				parent := lc.NewTable()
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
			val:       lc.NewTable(),
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
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	t.Run("Success", func(t *testing.T) {
		lc.Push(lc.NewFunction(lc.SerializeToJSON))
		tbl := lc.NewTable()
		tbl.RawSetString("key", lua.LString("value"))
		lc.Push(tbl)
		lc.Push(lua.LString(""))

		if err := lc.PCall(2, 2, nil); err != nil {
			t.Fatalf("unexpected lua error: %v", err)
		}

		errRes := lc.Get(-1)
		valRes := lc.Get(-2)
		lc.Pop(2)

		if errRes != lua.LNil {
			t.Fatalf("expected nil error, got %v", errRes)
		}

		if valRes.String() != `{"key":"value"}` {
			t.Errorf("unexpected json output: %s", valRes.String())
		}
	})

	t.Run("Failure", func(t *testing.T) {
		lc.Push(lc.NewFunction(lc.SerializeToJSON))
		tbl := lc.NewTable()
		tbl.RawSetInt(1, lua.LNumber(1))
		tbl.RawSetString("key", lua.LString("value"))
		lc.Push(tbl)

		if err := lc.PCall(1, 2, nil); err != nil {
			t.Fatalf("unexpected lua error: %v", err)
		}

		errRes := lc.Get(-1)
		valRes := lc.Get(-2)
		lc.Pop(2)

		if errRes == lua.LNil {
			t.Fatalf("expected error, got nil")
		}
		if valRes != lua.LNil {
			t.Errorf("expected value to be nil on error, got %v", valRes)
		}
	})
}

func TestJSONParser_ParseLiteral_JSONNumber(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	p := lc.parser

	t.Run("Valid numeric string", func(t *testing.T) {
		val, err := p.parseLiteral(json.Number("42.5"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.Type() != lua.LTNumber || float64(val.(lua.LNumber)) != 42.5 {
			t.Errorf("expected number 42.5, got %v (%v)", val, val.Type())
		}
	})

	t.Run("Invalid numeric string", func(t *testing.T) {
		val, err := p.parseLiteral(json.Number("not-a-number"))
		if err == nil {
			t.Fatalf("expected error, got nil (val=%v)", val)
		}
		if !strings.Contains(err.Error(), "Failed to convert JSON number") {
			t.Errorf("unexpected error message: %v", err)
		}
		if val != nil {
			t.Errorf("expected nil value on error, got %v", val)
		}
	})
}

func TestJSON_EmptyArrayRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		{name: "top-level empty array", script: `return (serialize((parse('[]'))))`, want: `[]`},
		{name: "empty array field", script: `return (serialize((parse('{"a":[]}'))))`, want: `{"a":[]}`},
		{name: "nested empty array", script: `return (serialize((parse('[[]]'))))`, want: `[[]]`},
		{name: "empty object stays object", script: `return (serialize((parse('{"a":{}}'))))`, want: `{"a":{}}`},
		{name: "empty Lua table stays object", script: `return (serialize({}))`, want: `{}`},
		{name: "array emptied after parse", script: `local t = parse('[1]'); table.remove(t, 1); return (serialize(t))`, want: `[]`},
		{name: "parsed empty array is still a plain table", script: `local t = parse('[]'); table.insert(t, 1); return tostring(type(t) == "table" and #t == 1)`, want: `true`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
			defer lc.Close()
			lc.SetGlobal("parse", lc.NewFunction(lc.ParseJSON))
			lc.SetGlobal("serialize", lc.NewFunction(lc.SerializeToJSON))

			if err := lc.DoString(tt.script); err != nil {
				t.Fatalf("unexpected lua error: %v", err)
			}
			got := lc.Get(-1).String()
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestJSONArray(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   string
	}{
		// --- Without an argument ---
		{
			name:   "no argument returns a new empty array",
			script: `local t, err = sinq.json.array(); assert(err == nil, tostring(err)); return (sinq.json.serialize(t))`,
			want:   `[]`,
		},
		{
			name:   "nil argument behaves like no argument",
			script: `local t, err = sinq.json.array(nil); assert(err == nil, tostring(err)); return (sinq.json.serialize(t))`,
			want:   `[]`,
		},
		{
			name: "each call returns a separate table and metatable",
			script: `
				local a = sinq.json.array()
				local b = sinq.json.array()
				assert(a ~= nil and b ~= nil, "expected tables")
				return tostring(a ~= b and getmetatable(a) ~= getmetatable(b))`,
			want: `true`,
		},
		{
			name: "returned empty array can be filled like a plain table",
			script: `
				local t = sinq.json.array()
				table.insert(t, 1); table.insert(t, "two")
				return (sinq.json.serialize(t))`,
			want: `[1,"two"]`,
		},
		// --- With an array-shaped table ---
		{
			name:   "returns the same table it was given",
			script: `local t = {}; local r, err = sinq.json.array(t); assert(err == nil, tostring(err)); return tostring(rawequal(r, t))`,
			want:   `true`,
		},
		{
			name:   "marks a given empty table in place",
			script: `local t = {}; sinq.json.array(t); return (sinq.json.serialize(t))`,
			want:   `[]`,
		},
		{
			name: "marked array that is emptied later serializes as an array",
			script: `
				local t = { 1, 2 }
				local _, err = sinq.json.array(t); assert(err == nil, tostring(err))
				table.remove(t); table.remove(t)
				return (sinq.json.serialize(t))`,
			want: `[]`,
		},
		{
			name:   "non-empty array stays the same",
			script: `local t, err = sinq.json.array({ 1, "two" }); assert(err == nil, tostring(err)); return (sinq.json.serialize(t))`,
			want:   `[1,"two"]`,
		},
		{
			name: "keeps an existing metatable and its fields",
			script: `
				local mt = { __index = function() return "dflt" end }
				local t = setmetatable({}, mt)
				local r, err = sinq.json.array(t); assert(err == nil, tostring(err))
				return tostring(getmetatable(r) == mt) .. " " .. r.missing .. " " .. (sinq.json.serialize(r))`,
			want: `true dflt []`,
		},
		{
			name: "marks an empty table parsed from a JSON object",
			script: `
				local t = sinq.json.parse('{}')
				local r, err = sinq.json.array(t); assert(err == nil, tostring(err))
				return (sinq.json.serialize(r))`,
			want: `[]`,
		},
		// --- With a table that isn't array-shaped ---
		{
			name:   "string keys are rejected",
			script: `local r, err = sinq.json.array({ a = 1 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "mixed array and string keys are rejected",
			script: `local r, err = sinq.json.array({ 1, a = 2 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "gaps in the integer keys are rejected",
			script: `local r, err = sinq.json.array({ [1] = 1, [3] = 3 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "array not starting at 1 is rejected",
			script: `local r, err = sinq.json.array({ [2] = 2 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "non-integer number keys are rejected",
			script: `local r, err = sinq.json.array({ [1.5] = 1 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "non-integer number keys with sparse arrays are rejected",
			script: `local r, err = sinq.json.array({ [1] = 1, [3] = 3, [1.5] = 1 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "out-of-range integer key filling a gap is rejected",
			script: `local r, err = sinq.json.array({ [1] = 1, [3] = 3, [0] = 0 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "boolean keys are rejected",
			script: `local r, err = sinq.json.array({ [true] = 1 }); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name:   "more than one argument is rejected",
			script: `local r, err = sinq.json.array({}, {}); return tostring(r == nil and err ~= nil)`,
			want:   `true`,
		},
		{
			name: "rejected table is not marked",
			script: `
				local t = { a = 1 }
				sinq.json.array(t)
				t.a = nil
				return (sinq.json.serialize(t))`,
			want: `{}`,
		},
		{
			name: "non-table argument fails",
			script: `
				local ok, r, err = pcall(sinq.json.array, "not a table")
				return tostring(not ok or (r == nil and err ~= nil))`,
			want: `true`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
			defer lc.Close()
			lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

			if err := lc.DoString(tt.script); err != nil {
				t.Fatalf("unexpected lua error: %v", err)
			}
			got := lc.Get(-1).String()
			if got != tt.want {
				t.Errorf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestJSONSerialize_RejectsGapFilledByOutOfRangeKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "zero key", key: `0`},
		{name: "negative key", key: `-1`},
		{name: "key past the array part", key: `2^40`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
			defer lc.Close()
			lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

			script := `local r, err = sinq.json.serialize({ [1] = "a", [3] = "c", [` + tt.key + `] = "z" }); return tostring(r) .. " | " .. tostring(err)`
			if err := lc.DoString(script); err != nil {
				t.Fatalf("unexpected lua error: %v", err)
			}
			got := lc.Get(-1).String()
			if !strings.HasPrefix(got, "nil | ") || strings.HasSuffix(got, "| nil") {
				t.Errorf("expected a serialization error for a sparse array, got %s", got)
			}
		})
	}
}

func TestSerializeToJSON_IndentArgument(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	steps := []struct {
		name string
		run  func() (string, error)
		want string
	}{
		{
			name: "serialize with indent is pretty-printed",
			run: func() (string, error) {
				err := lc.DoString(`return (sinq.json.serialize({ a = 1 }, "  "))`)
				return lc.Get(-1).String(), err
			},
			want: "{\n  \"a\": 1\n}",
		},
		{
			name: "Go-side SerializeLValue after an indented call is compact",
			run: func() (string, error) {
				tbl := lc.NewTable()
				tbl.RawSetString("b", lua.LNumber(2))
				return lc.SerializeLValue(tbl)
			},
			want: `{"b":2}`,
		},
		{
			name: "serialize with indent still pretty-prints after SerializeLValue",
			run: func() (string, error) {
				err := lc.DoString(`return (sinq.json.serialize({ c = 3 }, "\t"))`)
				return lc.Get(-1).String(), err
			},
			want: "{\n\t\"c\": 3\n}",
		},
		{
			name: "serialize without indent is compact after an indented call",
			run: func() (string, error) {
				err := lc.DoString(`return (sinq.json.serialize({ d = 4 }))`)
				return lc.Get(-1).String(), err
			},
			want: `{"d":4}`,
		},
	}

	for _, step := range steps {
		got, err := step.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", step.name, err)
		}
		if got != step.want {
			t.Errorf("%s: expected %q, got %q", step.name, step.want, got)
		}
	}
}
