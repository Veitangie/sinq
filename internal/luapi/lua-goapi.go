// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	lua "github.com/yuin/gopher-lua"
)

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

func ExtractBodyJson(ls *lua.LState) int {
	res, err := extractBodyJsonCore(ls)
	ls.Push(res)
	if err != nil {
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LNil)
	}
	return 2
}

func ExtractBodyJsonUnsafe(ls *lua.LState) int {
	res, err := extractBodyJsonCore(ls)
	if err != nil {
		ls.Error(lua.LString(err.Error()), 1)
		return 0
	}
	ls.Push(res)
	return 1
}

func extractBodyJsonCore(ls *lua.LState) (lua.LValue, error) {
	if request, ok := ls.Get(lua.UpvalueIndex(1)).(*lua.LTable); ok {
		if cached := request.RawGetString("bodyJson"); cached != lua.LNil {
			return cached, nil
		}

		bodyRaw, ok := request.RawGetString("bodyRaw").(lua.LString)
		if !ok {
			return lua.LNil, errors.New("Failed to extract body as json: bodyRaw not found or not a string")
		}

		var bodyJson any
		err := json.Unmarshal([]byte(bodyRaw), &bodyJson)
		if err != nil {
			return lua.LNil, fmt.Errorf("Failed to extract body as json: %s", err.Error())
		}

		bodyLua := ToLuaValue(bodyJson, ls)
		request.RawSetString("bodyJson", bodyLua)

		return bodyLua, nil
	}

	return lua.LNil, errors.New("Failed to extract body as json: no request table found")

}
