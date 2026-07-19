// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	lua "github.com/yuin/gopher-lua"
)

const ISO8601FormatMs = "2006-01-02T15:04:05.000Z07:00"

func FromLuaValue(value lua.LValue, visited map[*lua.LTable]bool) any {
	switch typedValue := value.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(typedValue)
	case lua.LNumber:
		return float64(typedValue)
	case lua.LString:
		return string(typedValue)
	case *lua.LTable:
		if visited[typedValue] {
			return nil
		}
		visited[typedValue] = true
		defer delete(visited, typedValue)

		if size := typedValue.Len(); size > 0 {
			res := make([]any, 0, size)
			typedValue.ForEach(func(key, value lua.LValue) {
				if len(res) == size {
					return
				}
				res = append(res, FromLuaValue(value, visited))
			})
			return res
		}

		res := make(map[string]any)
		typedValue.ForEach(func(key, value lua.LValue) {
			if stringKey, ok := key.(lua.LString); ok {
				res[string(stringKey)] = FromLuaValue(value, visited)
			}
		})
		return res
	case *lua.LUserData:
		return typedValue.Value
	}
	return nil
}

func ToLuaValue(value any, ls *lua.LState) lua.LValue {
	if value == nil {
		return lua.LNil
	}

	switch v := value.(type) {
	case bool:
		return lua.LBool(v)

	case float64:
		return lua.LNumber(v)

	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)

	case string:
		return lua.LString(v)

	case []any:
		tbl := ls.NewTable()
		for _, item := range v {
			tbl.Append(ToLuaValue(item, ls))
		}
		return tbl

	case map[string]any:
		tbl := ls.NewTable()
		for key, val := range v {
			tbl.RawSetString(key, ToLuaValue(val, ls))
		}
		return tbl

	case http.Header:
		tbl := ls.NewTable()
		for key, values := range v {
			if len(values) == 1 {
				tbl.RawSetString(key, lua.LString(values[0]))
			} else if len(values) > 1 {
				valArray := ls.NewTable()
				for _, val := range values {
					valArray.Append(lua.LString(val))
				}
				tbl.RawSetString(key, valArray)
			} else {
				tbl.RawSetString(key, lua.LString(""))
			}
		}
		return tbl

	default:
		return lua.LNil
	}
}

func (lc *LuaContext) ExtractBodyJson(ls *lua.LState) int {
	res, err := lc.extractBodyJsonCore(ls)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(res)
		ls.Push(lua.LNil)
	}
	return 2
}

func (lc *LuaContext) ExtractBodyJsonUnsafe(ls *lua.LState) int {
	res, err := lc.extractBodyJsonCore(ls)
	if err != nil {
		ls.Error(lua.LString(err.Error()), 1)
		return 0
	}
	ls.Push(res)
	return 1
}

func (lc *LuaContext) extractBodyJsonCore(ls *lua.LState) (lua.LValue, error) {
	if response, ok := ls.Get(lua.UpvalueIndex(1)).(*lua.LTable); ok {
		if cached := response.RawGetString("bodyJson"); cached != lua.LNil {
			return cached, nil
		}

		bodyRaw, ok := response.RawGetString("bodyRaw").(lua.LString)
		if !ok {
			return lua.LNil, errors.New("Failed to extract body as json: bodyRaw not found or not a string")
		}

		bodyLua, err := lc.parser.Parse(string(bodyRaw))

		if err != nil {
			return lua.LNil, fmt.Errorf("Failed to extract body as json: %s", err.Error())
		}

		response.RawSetString("bodyJson", bodyLua)

		return bodyLua, nil
	}

	return lua.LNil, errors.New("Failed to extract body as json: no request table found")

}

func (lc *LuaContext) Now(ls *lua.LState) int {
	ls.Push(lua.LNumber(lc.clock.Now().UnixMilli()))
	return 1
}

func TimeFromString(ls *lua.LState) int {
	source := ls.CheckString(1)
	format := ls.OptString(2, ISO8601FormatMs)

	resTime, err := time.Parse(format, source)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LNumber(resTime.UnixMilli()))
		ls.Push(lua.LNil)
	}

	return 2
}

func TimeToString(ls *lua.LState) int {
	source := ls.CheckInt64(1)
	format := ls.OptString(2, ISO8601FormatMs)

	ls.Push(lua.LString(time.UnixMilli(source).UTC().Format(format)))

	return 1
}
