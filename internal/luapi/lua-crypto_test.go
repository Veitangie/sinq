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
	"encoding/hex"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func runHashFunc(t *testing.T, L *lua.LState, fn lua.LGFunction, args ...string) string {
	L.Push(L.NewFunction(fn))
	for _, arg := range args {
		L.Push(lua.LString(arg))
	}
	if err := L.PCall(len(args), 1, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}
	res := L.Get(-1)
	L.Pop(1)
	if res.Type() != lua.LTString {
		t.Fatalf("expected string result, got %v", res.Type())
	}
	return res.String()
}

func TestLuaCrypto_Base64(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("Encode", func(t *testing.T) {
		got := runHashFunc(t, L, Base64Encode, "hello world")
		if got != "aGVsbG8gd29ybGQ=" {
			t.Errorf("expected aGVsbG8gd29ybGQ= got %s", got)
		}
	})

	t.Run("Decode Success", func(t *testing.T) {
		L.Push(L.NewFunction(Base64Decode))
		L.Push(lua.LString("aGVsbG8gd29ybGQ="))
		if err := L.PCall(1, 2, nil); err != nil {
			t.Fatal(err)
		}
		errRes := L.Get(-1)
		valRes := L.Get(-2)
		L.Pop(2)

		if errRes != lua.LNil {
			t.Errorf("expected nil error, got %v", errRes)
		}
		if valRes.String() != "hello world" {
			t.Errorf("expected hello world, got %s", valRes.String())
		}
	})

	t.Run("UrlEncode", func(t *testing.T) {
		got := runHashFunc(t, L, Base64UrlEncode, "hello world")
		if got != "aGVsbG8gd29ybGQ=" {
			t.Errorf("expected aGVsbG8gd29ybGQ= got %s", got)
		}
	})

	t.Run("UrlDecode Success", func(t *testing.T) {
		L.Push(L.NewFunction(Base64UrlDecode))
		L.Push(lua.LString("aGVsbG8gd29ybGQ="))
		if err := L.PCall(1, 2, nil); err != nil {
			t.Fatal(err)
		}
		errRes := L.Get(-1)
		valRes := L.Get(-2)
		L.Pop(2)

		if errRes != lua.LNil {
			t.Errorf("expected nil error, got %v", errRes)
		}
		if valRes.String() != "hello world" {
			t.Errorf("expected hello world, got %s", valRes.String())
		}
	})
}

func TestLuaCrypto_Hex(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("Encode", func(t *testing.T) {
		got := runHashFunc(t, L, HexEncode, "hello")
		if got != "68656c6c6f" {
			t.Errorf("expected 68656c6c6f got %s", got)
		}
	})

	t.Run("Decode Success", func(t *testing.T) {
		L.Push(L.NewFunction(HexDecode))
		L.Push(lua.LString("68656c6c6f"))
		if err := L.PCall(1, 2, nil); err != nil {
			t.Fatal(err)
		}
		errRes := L.Get(-1)
		valRes := L.Get(-2)
		L.Pop(2)

		if errRes != lua.LNil {
			t.Errorf("expected nil error, got %v", errRes)
		}
		if valRes.String() != "hello" {
			t.Errorf("expected hello, got %s", valRes.String())
		}
	})
}

func TestLuaCrypto_Hashes(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	input := "test payload"

	t.Run("MD5", func(t *testing.T) {
		expected := md5.Sum([]byte(input))
		if got := runHashFunc(t, L, MD5Hash, input); got != hex.EncodeToString(expected[:]) {
			t.Errorf("md5 default mismatch: expected %v, got %v", hex.EncodeToString(expected[:]), got)
		}
		if got := runHashFunc(t, L, MD5Hash, input, "raw"); got != string(expected[:]) {
			t.Errorf("md5 raw mismatch")
		}
	})

	t.Run("SHA1", func(t *testing.T) {
		expected := sha1.Sum([]byte(input))
		if got := runHashFunc(t, L, SHA1Hash, input); got != hex.EncodeToString(expected[:]) {
			t.Errorf("sha1 default mismatch")
		}
		if got := runHashFunc(t, L, SHA1Hash, input, "raw"); got != string(expected[:]) {
			t.Errorf("sha1 raw mismatch")
		}
	})

	t.Run("SHA256", func(t *testing.T) {
		expected := sha256.Sum256([]byte(input))
		if got := runHashFunc(t, L, SHA256Hash, input); got != hex.EncodeToString(expected[:]) {
			t.Errorf("sha256 default mismatch")
		}
		if got := runHashFunc(t, L, SHA256Hash, input, "raw"); got != string(expected[:]) {
			t.Errorf("sha256 raw mismatch")
		}
	})

	t.Run("SHA512", func(t *testing.T) {
		expected := sha512.Sum512([]byte(input))
		if got := runHashFunc(t, L, SHA512Hash, input); got != hex.EncodeToString(expected[:]) {
			t.Errorf("sha512 default mismatch")
		}
		if got := runHashFunc(t, L, SHA512Hash, input, "raw"); got != string(expected[:]) {
			t.Errorf("sha512 raw mismatch")
		}
	})
}

func TestLuaCrypto_HMAC(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	input := "test payload"
	key := "secret"

	t.Run("HMAC-SHA256", func(t *testing.T) {
		L.Push(L.NewFunction(HMACHash))
		L.Push(lua.LString(input))
		L.Push(lua.LString("sha256"))
		L.Push(lua.LString("secret"))

		if err := L.PCall(3, 2, nil); err != nil {
			t.Fatal(err)
		}
		errRes := L.Get(-1)
		valRes := L.Get(-2)
		L.Pop(2)

		if errRes != lua.LNil {
			t.Fatalf("unexpected hmac error: %v", errRes)
		}

		mac := hmac.New(sha256.New, []byte(key))
		mac.Write([]byte(input))
		expectedHex := hex.EncodeToString(mac.Sum(nil))

		if valRes.String() != expectedHex {
			t.Errorf("expected %s got %s", expectedHex, valRes.String())
		}
	})
}
