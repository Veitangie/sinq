// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/timer"
)

const encodeNamespace = "encoding"

func TestUrlEncode(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cases := map[string]string{
		"hello world":     "hello+world",
		"a&b=c":           "a%26b%3Dc",
		"100%":            "100%25",
		"π":               "%CF%80",
		"plain":           "plain",
		"slash/and?query": "slash%2Fand%3Fquery",
	}
	for in, want := range cases {
		got := runHashFunc(t, L, UrlEncode, in)
		if got != want {
			t.Errorf("UrlEncode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUrlDecode(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	t.Run("roundtrip", func(t *testing.T) {
		L.Push(L.NewFunction(UrlDecode))
		L.Push(lua.LString("a%26b%3Dc+d"))
		if err := L.PCall(1, 2, nil); err != nil {
			t.Fatal(err)
		}
		errRes := L.Get(-1)
		valRes := L.Get(-2)
		L.Pop(2)

		if errRes != lua.LNil {
			t.Errorf("expected nil error, got %v", errRes)
		}
		if valRes.String() != "a&b=c d" {
			t.Errorf("expected %q, got %q", "a&b=c d", valRes.String())
		}
	})

	t.Run("malformed percent escape returns error", func(t *testing.T) {
		L.Push(L.NewFunction(UrlDecode))
		L.Push(lua.LString("bad%2Gvalue"))
		if err := L.PCall(1, 2, nil); err != nil {
			t.Fatal(err)
		}
		errRes := L.Get(-1)
		valRes := L.Get(-2)
		L.Pop(2)

		if valRes != lua.LNil {
			t.Errorf("expected nil result on malformed input, got %v", valRes)
		}
		if errRes == lua.LNil {
			t.Error("expected a non-nil error string on malformed input")
		}
	})
}

func TestEncodingDecoders_ErrorOnMalformedInput(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cases := map[string]struct {
		fn lua.LGFunction
		in string
	}{
		"base64Decode":    {Base64Decode, "not*valid*base64"},
		"base64UrlDecode": {Base64UrlDecode, "not*valid*base64"},
		"hexDecode":       {HexDecode, "zz"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			L.Push(L.NewFunction(tc.fn))
			L.Push(lua.LString(tc.in))
			if err := L.PCall(1, 2, nil); err != nil {
				t.Fatal(err)
			}
			errRes := L.Get(-1)
			valRes := L.Get(-2)
			L.Pop(2)
			if valRes != lua.LNil {
				t.Errorf("expected nil result on malformed input, got %v", valRes)
			}
			if errRes == lua.LNil {
				t.Error("expected a non-nil error string on malformed input")
			}
		})
	}
}

func TestEncodingNamespaceWiring(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	script := `
		local enc = sinq[ns]
		assert(type(enc) == "table", "sinq." .. ns .. " must be a table")

		for _, fn in ipairs({
			"base64Encode", "base64Decode",
			"base64UrlEncode", "base64UrlDecode",
			"hexEncode", "hexDecode",
			"urlEncode", "urlDecode",
		}) do
			assert(type(enc[fn]) == "function", "sinq." .. ns .. "." .. fn .. " must be a function")
		end

		-- moved out of sinq.crypto
		for _, fn in ipairs({ "base64Encode", "base64Decode", "hexEncode", "hexDecode" }) do
			assert(sinq.crypto[fn] == nil, "sinq.crypto." .. fn .. " should have been removed")
		end

		-- hashing helpers stay under sinq.crypto
		for _, fn in ipairs({ "md5", "sha1", "sha256", "sha512", "hmac" }) do
			assert(type(sinq.crypto[fn]) == "function", "sinq.crypto." .. fn .. " must still be a function")
		end

		-- functional smoke checks
		assert(enc.base64Encode("hi") == "aGk=", "base64Encode smoke check")
		assert(enc.urlEncode("a b") == "a+b", "urlEncode smoke check")
	`

	lc.SetGlobal("ns", lua.LString(encodeNamespace))
	if err := lc.DoString(script); err != nil {
		t.Fatalf("namespace wiring assertions failed: %v", err)
	}
}
