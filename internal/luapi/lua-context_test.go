// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/timer"
)

func dummyFunc(L *lua.LState) int { return 0 }

func TestLuaContext_PreScript_Lifecycle(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)
	lc.SetupRequestEnvironment(0)

	if lc.RequestTable.RawGetString("attach") != lua.LNil {
		t.Error("Expected req.attach to be nil before setup")
	}
	if lc.RequestTable.RawGetString("saveResponseTo") != lua.LNil {
		t.Error("Expected req.saveResponseTo to be nil before setup")
	}
	if lc.RequestTable.RawGetString("cache") != lua.LNil {
		t.Error("Expected req.cache to be nil before setup")
	}

	lc.SetupPreScript(dummyFunc, dummyFunc, dummyFunc, dummyFunc)

	if lc.RequestTable.RawGetString("attach").Type() != lua.LTFunction {
		t.Error("Expected req.attach to be a function after setup")
	}
	if lc.RequestTable.RawGetString("saveResponseTo").Type() != lua.LTFunction {
		t.Error("Expected req.saveResponseTo to be a function after setup")
	}
	if lc.RequestTable.RawGetString("cache").Type() != lua.LTFunction {
		t.Error("Expected req.cache to be a function after setup")
	}

	lc.TearDownPreScript()

	if lc.RequestTable.RawGetString("attach") != lua.LNil {
		t.Error("Expected req.attach to be nil after teardown")
	}
	if lc.RequestTable.RawGetString("saveResponseTo") != lua.LNil {
		t.Error("Expected req.saveResponseTo to be nil after teardown")
	}
	if lc.RequestTable.RawGetString("cache") != lua.LNil {
		t.Error("Expected req.cache to be nil after teardown")
	}
}

func TestBug_SaveToLeak(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)
	lc.SetupRequestEnvironment(0)

	lc.SetupPreScript(dummyFunc, dummyFunc, dummyFunc, dummyFunc)
	lc.TearDownPreScript()

	saveToVal := lc.RequestTable.RawGetString("saveResponseTo")
	if saveToVal.Type().String() != "nil" {
		t.Errorf("BUG EXPOSED: 'saveResponseTo' leaked! Expected LNil in requestTable, got %s", saveToVal.Type().String())
	}
}

func TestLuaContext_JSONParserEquality(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	if err := lc.DoString(`
		local parsed = sinq.json.parse("null")
		assert(parsed == sinq.json.null, "Expected parsed null to exactly equal json.null singleton")

		local tbl = sinq.json.parse('{"a": null}')
		assert(tbl.a == sinq.json.null, "Expected nested parsed null to exactly equal json.null singleton")
	`); err != nil {
		t.Fatalf("Lua JSON equality test failed: %v", err)
	}
}

func TestLuaContext_CryptoMD5Registered(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	expected := md5.Sum([]byte("test payload"))
	script := fmt.Sprintf(`
		local got = sinq.crypto.md5("test payload")
		assert(got == %q, "sinq.crypto.md5 returned an unexpected hash: " .. tostring(got))
	`, hex.EncodeToString(expected[:]))

	if err := lc.DoString(script); err != nil {
		t.Fatalf("sinq.crypto.md5 is not reachable through the registered Lua table: %v", err)
	}
}

func TestLuaContext_RecordResponse(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)
	lc.SetupRequestEnvironment(0)

	lc.RecordResponseMeta(1, 200, nil)
	if lc.ResponseTable.RawGetString("attempt") != lua.LNumber(1) {
		t.Errorf("Expected attempt to be 1")
	}
	if lc.ResponseTable.RawGetString("code") != lua.LNumber(200) {
		t.Errorf("Expected code to be 200")
	}

	lc.RecordResponseFile(1024)
	if lc.ResponseTable.RawGetString("size") != lua.LNumber(1024) {
		t.Errorf("Expected size to be 1024")
	}

	lc.RecordResponseBody([]byte("hello"), true)
	if lc.ResponseTable.RawGetString("oversized") != lua.LBool(true) {
		t.Errorf("Expected oversized to be true")
	}
	if lc.ResponseTable.RawGetString("bodyRaw") != lua.LString("hello") {
		t.Errorf("Expected bodyRaw to be hello")
	}
}

func TestLuaContext_SetupScripts(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	lc.SetupRetryScript()
	if lc.sinqTable.RawGetString("retry").Type() != lua.LTTable {
		t.Errorf("Expected retry table to be set")
	}
	lc.TearDownRetryScript()
	if lc.sinqTable.RawGetString("retry") != lua.LNil {
		t.Errorf("Expected retry table to be torn down")
	}

	lc.SetupAssertScript(dummyFunc)
	if lc.assertTable.RawGetString("fileMatches").Type() != lua.LTFunction {
		t.Errorf("Expected fileMatches function to be set")
	}
	lc.TearDownAssertScript()
	if lc.assertTable.RawGetString("fileMatches") != lua.LNil {
		t.Errorf("Expected fileMatches function to be torn down")
	}
}

func TestLuaContext_RunSandboxed(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()

	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	fn, err := lc.LoadString(`return 1 + 1`)
	if err != nil {
		t.Fatalf("Failed to load string: %v", err)
	}

	err = lc.RunSandboxed(fn.Proto, time.Second)
	if err != nil {
		t.Fatalf("RunSandboxed failed: %v", err)
	}
}

func TestLuaContext_PrintAndWrite(t *testing.T) {
	buf := &bytes.Buffer{}
	lc := NewLuaContext(timer.DefaultClock{}, true, nil, buf)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	err := lc.DoString(`
		print("hello", "world")
		io.write("foo", "bar")
	`)
	if err != nil {
		t.Fatalf("Lua execution failed: %v", err)
	}

	out := buf.String()
	expected := "hello\tworld\nfoobar"
	if out != expected {
		t.Errorf("Expected printed output %q, got %q", expected, out)
	}
}

func TestLuaContext_WriteNumber(t *testing.T) {
	buf := &bytes.Buffer{}
	lc := NewLuaContext(timer.DefaultClock{}, true, nil, buf)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	err := lc.DoString(`io.write(42, " ", 3.5)`)
	if err != nil {
		t.Fatalf("Lua execution failed: %v", err)
	}

	if out := buf.String(); out != "42 3.5" {
		t.Errorf("Expected printed output %q, got %q", "42 3.5", out)
	}
}

func TestLuaContext_RetryStopSentinel(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	stop := lc.retryTable.RawGetString("stop")
	if stop != lua.LNumber(-1) {
		t.Errorf("Expected sinq.retry.stop to be -1, got %v", stop)
	}
}

func TestLuaContext_TimeConstants(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	tests := []struct {
		key  string
		want lua.LNumber
	}{
		{"ms", 1},
		{"second", 1000},
		{"minute", 60000},
		{"hour", 3600000},
	}
	for _, tt := range tests {
		if got := lc.timeTable.RawGetString(tt.key); got != tt.want {
			t.Errorf("sinq.time.%s = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestLuaContext_SetupRequestEnvironment_ResponseIndex(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(dummyFunc, dummyFunc, dummyFunc, nil, nil)

	lc.SetupRequestEnvironment(2)

	if got := lc.allResponsesTable.RawGetInt(3); got != lc.ResponseTable {
		t.Errorf("expected response table to be recorded at index requestIdx+1=3, got %v", got)
	}
	if got := lc.allResponsesTable.RawGetInt(2); got == lc.ResponseTable {
		t.Errorf("did not expect the response table at index requestIdx=2")
	}
}
