// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"time"

	lua "github.com/yuin/gopher-lua"
)

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

func Sleep(ls *lua.LState) int {
	ms := ls.CheckInt64(1)
	select {
	case <-time.After(time.Duration(ms) * time.Millisecond):
	case <-ls.Context().Done():
	}
	return 0
}
