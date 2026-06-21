// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"net/http"
	"time"

	"github.com/Veitangie/sinq/internal/luapi"
	lua "github.com/yuin/gopher-lua"
)

type luaContext struct {
	lua.LState
	sandbox           *lua.LTable
	sinqTable         *lua.LTable
	assertTable       *lua.LTable
	retryTable        *lua.LTable
	allResponsesTable *lua.LTable
	requestTable      *lua.LTable
	responseTable     *lua.LTable
}

func newLuaContext() *luaContext {
	lc := luaContext{LState: *lua.NewState()}

	lc.sandbox = lc.NewTable()
	sandboxMeta := lc.NewTable()
	lc.SetField(sandboxMeta, "__index", lc.Get(lua.GlobalsIndex))
	lc.sandbox.Metatable = sandboxMeta

	return &lc
}

func (lc *luaContext) setupScenarioEnvironment(setIdx lua.LGFunction, finishScenario lua.LGFunction, failAssert lua.LGFunction, secrets any, env any) {
	lc.sandbox.ForEach(func(k, v lua.LValue) { lc.sandbox.RawSet(k, lua.LNil) })
	lc.sandbox.RawSetString("_G", lc.sandbox)

	lc.sinqTable = lc.NewTable()
	lc.sinqTable.RawSetString("setNextRequest", lc.NewFunction(setIdx))
	lc.sinqTable.RawSetString("finishScenario", lc.NewFunction(finishScenario))

	lc.allResponsesTable = lc.NewTable()
	lc.sinqTable.RawSetString("responses", lc.allResponsesTable)

	lc.sinqTable.RawSetString("ms", lua.LNumber(1))
	lc.sinqTable.RawSetString("second", lua.LNumber(1000))
	lc.sinqTable.RawSetString("minute", lua.LNumber(1000*60))
	lc.sinqTable.RawSetString("hour", lua.LNumber(1000*60*60))

	lc.retryTable = lc.NewTable()
	lc.retryTable.RawSetString("when", lc.NewFunctionFromProto(luapi.RetryWhen()))
	lc.retryTable.RawSetString("whenExponential", lc.NewFunctionFromProto(luapi.RetryExponential()))
	lc.retryTable.RawSetString("withJitter", lc.NewFunctionFromProto(luapi.RetryJitter()))
	lc.retryTable.RawSetString("stop", lua.LNumber(-1))

	lc.assertTable = lc.NewTable()
	lc.assertTable.RawSetString("fail", lc.NewFunction(failAssert))
	lc.assertTable.RawSetString("code", lc.NewFunctionFromProto(luapi.AssertCode()))
	lc.assertTable.RawSetString("equals", lc.NewFunctionFromProto(luapi.AssertEquals()))
	lc.assertTable.RawSetString("contains", lc.NewFunctionFromProto(luapi.AssertContains()))
	lc.assertTable.RawSetString("isTrue", lc.NewFunctionFromProto(luapi.AssertCondition()))

	lc.SetGlobal("secrets", luapi.ToLuaValue(secrets, &lc.LState))
	lc.SetGlobal("env", luapi.ToLuaValue(env, &lc.LState))

	lc.SetGlobal("sinq", lc.sinqTable)
}

func (lc *luaContext) setupRequestEnvironment(requestIdx int) {
	lc.requestTable = lc.NewTable()
	lc.responseTable = lc.NewTable()
	lc.allResponsesTable.RawSetInt(requestIdx+1, lc.responseTable)
	lc.SetGlobal("req", lc.requestTable)
	lc.SetGlobal("res", lc.responseTable)
}

func (lc *luaContext) runSandboxed(byteCode *lua.FunctionProto, timeout time.Duration) error {
	fn := lc.NewFunctionFromProto(byteCode)
	fn.Env = lc.sandbox

	lc.Push(fn)

	oldCtx := lc.Context()
	if oldCtx == nil {
		oldCtx = context.Background()
	}
	ctxWithTimeout, cancelCtx := context.WithTimeout(oldCtx, timeout)
	defer cancelCtx()

	lc.SetContext(ctxWithTimeout)
	err := lc.PCall(0, lua.MultRet, nil)
	lc.SetContext(oldCtx)
	return err
}

func (lc *luaContext) RecordResponseMeta(attempt int, code int, headers http.Header) {
	lc.responseTable.RawSetString("attempt", lua.LNumber(attempt))
	lc.responseTable.RawSetString("code", lua.LNumber(code))
	lc.responseTable.RawSetString("headers", luapi.ToLuaValue(headers, &lc.LState))
}

func (lc *luaContext) RecordResponseFile(written int64) {
	lc.responseTable.RawSetString("size", lua.LNumber(written))
}

func (lc *luaContext) RecordResponseBody(data []byte, oversized bool) {
	if oversized {
		lc.responseTable.RawSetString("oversized", lua.LTrue)
	} else {
		lc.responseTable.RawSetString("oversized", lua.LFalse)
	}

	lc.responseTable.RawSetString("bodyRaw", lua.LString(data))
	lc.responseTable.RawSetString("bodyJson", lua.LNil)

	lc.responseTable.RawSetString("extractBodyJson", lc.NewClosure(luapi.ExtractBodyJson, lc.responseTable))
	lc.responseTable.RawSetString("json", lc.NewClosure(luapi.ExtractBodyJsonUnsafe, lc.responseTable))
}

func (lc *luaContext) setupPreScript(attach lua.LGFunction, saveTo lua.LGFunction) {
	lc.requestTable.RawSetString(
		"attach",
		lc.NewFunction(attach),
	)
	lc.responseTable.RawSetString(
		"saveTo",
		lc.NewFunction(saveTo),
	)
}

func (lc *luaContext) tearDownPreScript() {
	lc.requestTable.RawSetString("attach", lua.LNil)
	lc.responseTable.RawSetString("saveTo", lua.LNil)
}

func (lc *luaContext) setupRetryScript() {
	lc.sinqTable.RawSetString("retry", lc.retryTable)
}

func (lc *luaContext) tearDownRetryScript() {
	lc.sinqTable.RawSetString("retry", lua.LNil)
}

func (lc *luaContext) setupAssertScript() {
	lc.sinqTable.RawSetString("assert", lc.assertTable)
}

func (lc *luaContext) tearDownAssertScript() {
	lc.sinqTable.RawSetString("assert", lua.LNil)
}
