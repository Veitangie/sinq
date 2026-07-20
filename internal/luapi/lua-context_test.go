// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/timer"
	lua "github.com/yuin/gopher-lua"
)

func dummyFunc(L *lua.LState) int { return 0 }

func TestLuaContext_PreScript_Lifecycle(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
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
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
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
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
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

func TestLuaContext_RecordResponse(t *testing.T) {
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
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
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
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
	lc := NewLuaContext(timer.DefaultClock{}, false, nil)
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
