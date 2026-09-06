// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func MD5Hash(ls *lua.LState) int {
	source := ls.CheckString(1)
	encStr := strings.ToLower(ls.OptString(2, "hex"))
	enc, ok := resultEncoder[encStr]
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown encoding algorithm: %s", encStr)))
		return 2
	}
	hash := md5.Sum([]byte(source))
	ls.Push(lua.LString(enc(hash[:])))
	ls.Push(lua.LNil)
	return 2
}

func SHA1Hash(ls *lua.LState) int {
	source := ls.CheckString(1)
	encStr := strings.ToLower(ls.OptString(2, "hex"))
	enc, ok := resultEncoder[encStr]
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown encoding algorithm: %s", encStr)))
		return 2
	}
	hash := sha1.Sum([]byte(source))
	ls.Push(lua.LString(enc(hash[:])))
	ls.Push(lua.LNil)
	return 2
}

func SHA256Hash(ls *lua.LState) int {
	source := ls.CheckString(1)
	encStr := strings.ToLower(ls.OptString(2, "hex"))
	enc, ok := resultEncoder[encStr]
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown encoding algorithm: %s", encStr)))
		return 2
	}
	hash := sha256.Sum256([]byte(source))
	ls.Push(lua.LString(enc(hash[:])))
	ls.Push(lua.LNil)
	return 2
}

func SHA512Hash(ls *lua.LState) int {
	source := ls.CheckString(1)
	encStr := strings.ToLower(ls.OptString(2, "hex"))
	enc, ok := resultEncoder[encStr]
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown encoding algorithm: %s", encStr)))
		return 2
	}
	hash := sha512.Sum512([]byte(source))
	ls.Push(lua.LString(enc(hash[:])))
	ls.Push(lua.LNil)
	return 2
}

func HMACHash(ls *lua.LState) int {
	source := ls.CheckString(1)
	algo := ls.OptString(2, "sha256")
	key := ls.OptString(3, "")
	encStr := strings.ToLower(ls.OptString(4, "hex"))
	enc, ok := resultEncoder[encStr]
	if !ok {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown encoding algorithm: %s", encStr)))
		return 2
	}

	var algoHash func() hash.Hash
	switch strings.ToLower(algo) {
	case "sha256":
		algoHash = sha256.New
	case "sha1":
		algoHash = sha1.New
	case "sha512":
		algoHash = sha512.New
	case "md5":
		algoHash = md5.New
	default:
		ls.Push(lua.LNil)
		ls.Push(lua.LString(fmt.Sprintf("Unknown hash algorithm: %s", algo)))
		return 2
	}

	hmacer := hmac.New(algoHash, []byte(key))
	hmacer.Write([]byte(source))
	res := hmacer.Sum(nil)

	ls.Push(lua.LString(enc(res)))
	ls.Push(lua.LNil)
	return 2
}
