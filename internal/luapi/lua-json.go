// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const asEmptyArrayMeta string = "asEmptyArray"

var jsonNullId = &struct{}{}

func newJSONNull(L *lua.LState) lua.LValue {
	ud := L.NewUserData()
	ud.Value = jsonNullId

	mt := L.NewTable()
	L.SetField(mt, "__tostring", L.NewFunction(func(L *lua.LState) int {
		L.Push(lua.LString("null"))
		return 1
	}))

	ud.Metatable = mt

	return ud
}

func isJSONNull(val *lua.LUserData) bool {
	return val.Value == jsonNullId
}

type JSONParser struct {
	L     *lua.LState
	LNull lua.LValue
}

func (p JSONParser) Parse(source string) (lua.LValue, error) {

	if p.L == nil {
		return nil, errors.New("Failed to parse data to Lua: LState is nil")
	}

	dec := json.NewDecoder(strings.NewReader(source))
	token, err := dec.Token()
	if err != nil {
		return nil, err
	}
	res, err := p.parseValue(token, dec)
	if err != nil {
		return nil, err
	}

	if token, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("Expected JSON to end, got %v", token)
	}

	return res, nil
}

func (p JSONParser) parseValue(token json.Token, source *json.Decoder) (value lua.LValue, err error) {
	switch v := token.(type) {
	case json.Delim:
		switch v {
		case '{':
			value, err = p.parseObject(source)

		case '[':
			value, err = p.parseArray(source)

		default:
			value, err = nil, fmt.Errorf("Expected '[' or '{' at the start of JSON value, got %c", v)
		}

	default:
		value, err = p.parseLiteral(token)
	}

	return
}

func (p JSONParser) parseObject(source *json.Decoder) (lua.LValue, error) {
	result := p.L.NewTable()

	for {
		key, err := source.Token()
		if err != nil {
			return nil, err
		}

		switch v := key.(type) {
		case json.Delim:
			switch v {
			case '}':
				return result, nil

			default:
				return nil, fmt.Errorf("Expected '}' at the end of JSON object, got %c", v)
			}

		case string:
			token, err := source.Token()
			if err != nil {
				return nil, err
			}

			value, err := p.parseValue(token, source)
			if err != nil {
				return nil, err
			}

			result.RawSetString(v, value)

		default:
			return nil, fmt.Errorf("Expected key in object, got %v", v)
		}
	}
}

func (p JSONParser) parseArray(source *json.Decoder) (lua.LValue, error) {
	result := p.L.NewTable()
	meta := p.L.NewTable()
	meta.RawSetString(asEmptyArrayMeta, lua.LTrue)
	result.Metatable = meta
	resIdx := 1
	for {
		token, err := source.Token()
		if err != nil {
			return nil, err
		}

		if delim, ok := token.(json.Delim); ok && delim == ']' {
			return result, nil
		}

		value, err := p.parseValue(token, source)
		if err != nil {
			return nil, err
		}

		result.RawSetInt(resIdx, value)
		resIdx += 1
	}
}

func (p JSONParser) parseLiteral(token json.Token) (lua.LValue, error) {
	if token == nil {
		return p.LNull, nil
	}

	switch v := token.(type) {
	case string:
		return lua.LString(v), nil

	case float64:
		return lua.LNumber(v), nil

	case json.Number:
		value, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return nil, fmt.Errorf("Failed to convert JSON number to Lua number: %w", err)
		}
		return lua.LNumber(value), err

	case bool:
		return lua.LBool(v), nil

	default:
		return nil, fmt.Errorf("Unknown JSON literal: %v", token)
	}
}

type JSONSerializer struct {
	sb      strings.Builder
	indent  string
	newline bool
	visited map[*lua.LTable]bool
}

func (s *JSONSerializer) SerializeLValue(val lua.LValue) (string, error) {
	s.visited = make(map[*lua.LTable]bool)

	err := s.serializeValue(val, "")

	defer func() {
		if s.sb.Cap() > 1<<20 {
			s.sb = strings.Builder{}
		} else {
			s.sb.Reset()
		}
		s.visited = nil
	}()

	if err != nil {
		return "", err
	}
	return s.sb.String(), nil
}

type keyval struct {
	key string
	val lua.LValue
}

func (s *JSONSerializer) serializeValue(val lua.LValue, indent string) error {
	switch valTyped := val.(type) {
	case *lua.LTable:
		if s.visited[valTyped] {
			return errors.New("Cycle detected, unable to serialize")
		}

		s.visited[valTyped] = true
		defer delete(s.visited, valTyped)
		return s.serializeTable(valTyped, indent)
	default:
		return s.serializePrimitive(valTyped)
	}
}

func (s *JSONSerializer) serializePrimitive(val lua.LValue) error {
	switch valTyped := val.(type) {
	case *lua.LUserData:
		if isJSONNull(valTyped) {
			s.sb.WriteString("null")
			return nil
		}
	case lua.LNumber:
		float := float64(valTyped)
		if math.IsInf(float, 0) || math.IsNaN(float) {
			return fmt.Errorf("Invalid JSON number: %v", float)
		}

		s.sb.WriteString(strconv.FormatFloat(float, 'f', -1, 64))
		return nil
	case lua.LBool:
		if bool(valTyped) {
			s.sb.WriteString("true")
		} else {
			s.sb.WriteString("false")
		}
		return nil
	case lua.LString:
		res, _ := json.Marshal(string(valTyped))
		s.sb.Write(res)
		return nil
	}
	return fmt.Errorf("Expected type one of 'nil', 'number', 'bool', 'string', got: '%v'", val.Type())
}

func (s *JSONSerializer) serializeArray(array []lua.LValue, indent string) error {
	s.sb.WriteByte('[')
	if s.newline {
		s.sb.WriteByte('\n')
		s.sb.WriteString(indent)
		s.sb.WriteString(s.indent)
	}

	for idx, arrEntry := range array {
		err := s.serializeValue(arrEntry, indent+s.indent)
		if err != nil {
			return err
		}

		if idx < len(array)-1 {
			s.sb.WriteByte(',')
		}

		if s.newline {
			s.sb.WriteByte('\n')
			if idx < len(array)-1 {
				s.sb.WriteString(s.indent)
			}
			s.sb.WriteString(indent)
		}
	}
	s.sb.WriteByte(']')
	return nil
}

func (s *JSONSerializer) serializeObject(tableEntries []keyval, indent string) error {
	slices.SortFunc(tableEntries, func(one, two keyval) int { return strings.Compare(one.key, two.key) })

	s.sb.WriteByte('{')
	if s.newline {
		s.sb.WriteByte('\n')
		s.sb.WriteString(indent)
		s.sb.WriteString(s.indent)
	}

	for idx, keyval := range tableEntries {
		safeKey, _ := json.Marshal(keyval.key)
		s.sb.Write(safeKey)
		s.sb.WriteByte(':')

		if s.indent != "" {
			s.sb.WriteByte(' ')
		}

		err := s.serializeValue(keyval.val, indent+s.indent)
		if err != nil {
			return fmt.Errorf("%s:%w", keyval.key, err)
		}

		if idx < len(tableEntries)-1 {
			s.sb.WriteByte(',')
		}

		if s.newline {
			s.sb.WriteByte('\n')
			if idx < len(tableEntries)-1 {
				s.sb.WriteString(s.indent)
			}
			s.sb.WriteString(indent)
		}
	}

	s.sb.WriteByte('}')
	return nil
}

func disassembleTable(tbl *lua.LTable) (arrayEntries []lua.LValue, tableEntries []keyval, err error) {
	arrLen := tbl.Len()
	arrayEntries = make([]lua.LValue, 0, arrLen)
	tableEntries = make([]keyval, 0)
	errorsAcc := make([]string, 0)

	tbl.ForEach(func(key, val lua.LValue) {
		switch typedKey := key.(type) {
		case lua.LNumber:
			if len(arrayEntries) == arrLen {
				errorsAcc = append(errorsAcc, fmt.Sprintf("Unexpected number key %v", typedKey))
				return
			}

			if lua.LNumber(int(typedKey)) != typedKey {
				errorsAcc = append(errorsAcc, "Non-whole number key in array. Make sure to use continuous arrays or string-only keys in serialized tables")
				return
			}

			if typedKey <= 0 || typedKey > lua.LNumber(arrLen) {
				errorsAcc = append(errorsAcc, "Found sparse array, expected continuous")
				return
			}

			arrayEntries = append(arrayEntries, val)
		case lua.LString:
			tableEntries = append(tableEntries, keyval{key: string(typedKey), val: val})
		default:
			errorsAcc = append(errorsAcc, fmt.Sprintf("Unexpected key %v of type %v. Make sure to use string keys in serialized tables", key, key.Type()))
		}
	})

	if len(arrayEntries) != arrLen {
		errorsAcc = append(errorsAcc, "Found sparse array, expected continuous")
	}

	if len(arrayEntries) != 0 && len(tableEntries) != 0 {
		errorsAcc = append(errorsAcc, "Found both list and table entries")
	}

	if len(errorsAcc) != 0 {
		arrayEntries = []lua.LValue{}
		tableEntries = []keyval{}
		err = errors.New(strings.Join(errorsAcc, ",\n"))
	}

	return
}

func (s *JSONSerializer) serializeTable(tbl *lua.LTable, indent string) error {
	arrayEntries, tableEntries, err := disassembleTable(tbl)

	if err != nil {
		return err
	}

	if len(arrayEntries) != 0 {
		return s.serializeArray(arrayEntries, indent)
	}

	if len(tableEntries) != 0 {
		return s.serializeObject(tableEntries, indent)
	}

	if meta, ok := tbl.Metatable.(*lua.LTable); ok {
		asEmptyArray, ok := meta.RawGetString(asEmptyArrayMeta).(lua.LBool)
		if ok && bool(asEmptyArray) {
			s.sb.WriteByte('[')
			s.sb.WriteByte(']')
			return nil
		}
	}

	s.sb.WriteByte('{')
	s.sb.WriteByte('}')
	return nil
}

func (lc *LuaContext) ParseJSON(l *lua.LState) int {
	source := l.CheckString(1)
	res, err := lc.parser.Parse(string(source))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
	} else {
		l.Push(res)
		l.Push(lua.LNil)
	}
	return 2
}

func (lc *LuaContext) SerializeLValue(value lua.LValue) (string, error) {
	lc.serializer.indent = ""
	lc.serializer.newline = false
	return lc.serializer.SerializeLValue(value)
}

func (lc *LuaContext) SerializeToJSON(l *lua.LState) int {
	source := l.CheckAny(1)
	indent := l.OptString(2, "")

	lc.serializer.indent = indent
	lc.serializer.newline = indent != ""
	res, err := lc.serializer.SerializeLValue(source)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
	} else {
		l.Push(lua.LString(res))
		l.Push(lua.LNil)
	}

	return 2
}

func JSONArray(l *lua.LState) int {
	count := l.GetTop()
	if count > 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("sinq.json.array: Too many arguments, expecting 0 or 1"))
		return 2
	}

	var table *lua.LTable
	switch typed := l.Get(1).(type) {
	case *lua.LNilType:
		table = l.NewTable()
	case *lua.LTable:
		table = typed
	default:
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("sinq.json.array: Expected table or nil, got %v", typed.Type())))
		return 2
	}

	_, tableEntries, err := disassembleTable(table)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("sinq.json.array: %s", err.Error())))
		return 2
	}

	if lenTable := len(tableEntries); lenTable != 0 {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("sinq.json.array: Found %d key-value entries, cannot mark as array", lenTable)))
		return 2
	}

	meta, ok := table.Metatable.(*lua.LTable)
	if !ok {
		if _, ok := table.Metatable.(*lua.LNilType); !ok {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("sinq.json.array: Expecting metatable to be nil or a table, got %s", table.Metatable.Type())))
			return 2
		}
		meta = l.NewTable()
		table.Metatable = meta
	}

	meta.RawSetString(asEmptyArrayMeta, lua.LTrue)
	l.Push(table)
	l.Push(lua.LNil)
	return 2
}
