// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"fmt"
	"regexp"

	lua "github.com/yuin/gopher-lua"
)

func compileRegex(ls *lua.LState) int {
	exprRaw := ls.Get(1)
	expr, ok := exprRaw.(lua.LString)
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("sinq.regex.compile: expected argument to be string, got: %s", exprRaw.Type().String())))
		return 2
	}
	reg, err := regexp.Compile(string(expr))
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("sinq.regex.compile: %s", err.Error())))
		return 2
	}
	allGroups := reg.SubexpNames()
	allGroupsMap := make(map[string]bool, len(allGroups))
	for _, group := range allGroups {
		if group == "full" {
			ls.Push(lua.LNil)
			ls.Push(lua.LString("sinq.regex.compile: reserved subexpression name \"full\" cannot be used"))
			return 2
		}
		if group != "" && allGroupsMap[group] {
			ls.Push(lua.LNil)
			ls.Push(lua.LString(fmt.Sprintf("sinq.regex.compile: found duplicate subexpression name %s", group)))
			return 2
		}
		allGroupsMap[group] = true
	}
	resTable := ls.NewTable()
	resTable.RawSetString("match", ls.NewClosure(matchRegex(reg), resTable))
	resTable.RawSetString("matchAll", ls.NewClosure(matchAllRegex(reg), resTable))
	ls.Push(resTable)
	ls.Push(lua.LNil)
	return 2
}

func matchRegex(reg *regexp.Regexp) lua.LGFunction {
	return func(ls *lua.LState) int {
		stripSelf(ls)
		toMatch := ls.CheckString(1)
		ls.Push(convertMatch(ls, toMatch, reg.SubexpNames(), reg.FindStringSubmatchIndex(string(toMatch))))
		return 1
	}
}

func matchAllRegex(reg *regexp.Regexp) lua.LGFunction {
	return func(ls *lua.LState) int {
		stripSelf(ls)
		toMatch := ls.CheckString(1)
		matches := reg.FindAllStringSubmatchIndex(string(toMatch), -1)
		resTable := ls.NewTable()
		for idx, match := range matches {
			resTable.RawSetInt(idx+1, convertMatch(ls, toMatch, reg.SubexpNames(), match))
		}
		ls.Push(resTable)
		return 1
	}
}

func convertMatch(ls *lua.LState, source string, groupNames []string, matches []int) lua.LValue {
	if len(matches) <= 0 {
		return lua.LNil
	}

	res := ls.NewTable()
	res.RawSetString("full", lua.LString(source[matches[0]:matches[1]]))
	for idx := range (len(matches) / 2) - 1 {
		actualIdx := (idx + 1) * 2
		if matches[actualIdx] == -1 {
			res.RawSetInt(idx+1, lua.LFalse)
		} else {
			res.RawSetInt(idx+1, lua.LString(source[matches[actualIdx]:matches[actualIdx+1]]))
		}
	}
	for idx, name := range groupNames {
		if name == "" {
			continue
		}
		actualIdx := idx * 2
		if matches[actualIdx] == -1 {
			res.RawSetString(name, lua.LFalse)
		} else {
			res.RawSetString(name, lua.LString(source[matches[actualIdx]:matches[actualIdx+1]]))
		}
	}

	return res
}
