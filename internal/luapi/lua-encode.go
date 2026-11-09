// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"

	lua "github.com/yuin/gopher-lua"
)

var resultEncoder map[string]func([]byte) string = map[string]func([]byte) string{
	"hex":       hex.EncodeToString,
	"base64":    base64.StdEncoding.EncodeToString,
	"base64url": base64.URLEncoding.EncodeToString,
	"raw":       func(b []byte) string { return string(b) },
}

func Base64Encode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res := base64.StdEncoding.EncodeToString([]byte(source))
	ls.Push(lua.LString(res))
	return 1
}

func Base64Decode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res, err := base64.StdEncoding.DecodeString(source)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LString(res))
		ls.Push(lua.LNil)
	}
	return 2
}

func Base64UrlEncode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res := base64.URLEncoding.EncodeToString([]byte(source))
	ls.Push(lua.LString(res))
	return 1
}

func Base64UrlDecode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res, err := base64.URLEncoding.DecodeString(source)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LString(res))
		ls.Push(lua.LNil)
	}
	return 2
}

func HexEncode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res := hex.EncodeToString([]byte(source))
	ls.Push(lua.LString(res))
	return 1
}

func HexDecode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res, err := hex.DecodeString(source)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LString(res))
		ls.Push(lua.LNil)
	}

	return 2
}

func UrlEncode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res := url.QueryEscape(source)
	ls.Push(lua.LString(res))

	return 1
}

func UrlDecode(ls *lua.LState) int {
	source := ls.CheckString(1)
	res, err := url.QueryUnescape(source)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
	} else {
		ls.Push(lua.LString(res))
		ls.Push(lua.LNil)
	}

	return 2
}
