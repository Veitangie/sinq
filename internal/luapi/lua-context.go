// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/Veitangie/sinq/internal/timer"
	lua "github.com/yuin/gopher-lua"
)

type LuaContext struct {
	lua.LState
	clock             timer.Clock
	serializer        JSONSerializer
	parser            JSONParser
	generator         rand.Rand
	sandbox           *lua.LTable
	sinqTable         *lua.LTable
	assertTable       *lua.LTable
	retryTable        *lua.LTable
	allResponsesTable *lua.LTable
	jsonTable         *lua.LTable
	timeTable         *lua.LTable
	cryptoTable       *lua.LTable
	jwtTable          *lua.LTable
	fakeTable         *lua.LTable
	RequestTable      *lua.LTable
	ResponseTable     *lua.LTable
}

func NewLuaContext(clock timer.Clock, unrestricted bool, luaPath string) *LuaContext {
	if clock == nil {
		clock = timer.DefaultClock{}
	}
	var lc LuaContext
	if unrestricted {
		lc = LuaContext{LState: *lua.NewState()}
	} else {
		lc = LuaContext{LState: *lua.NewState(lua.Options{SkipOpenLibs: true})}
		lua.OpenBase(&lc.LState)
		lua.OpenChannel(&lc.LState)
		lua.OpenCoroutine(&lc.LState)
		lua.OpenDebug(&lc.LState)
		lua.OpenMath(&lc.LState)
		lua.OpenPackage(&lc.LState)
		lua.OpenString(&lc.LState)
		lua.OpenTable(&lc.LState)
	}

	lc.clock = clock

	if module, ok := lc.GetGlobal("package").(*lua.LTable); ok {
		lc.SetField(module, "path", lua.LString(luaPath))
	}

	lc.sandbox = lc.NewTable()
	sandboxMeta := lc.NewTable()
	lc.SetField(sandboxMeta, "__index", lc.Get(lua.GlobalsIndex))
	lc.sandbox.Metatable = sandboxMeta

	lc.parser = JSONParser{
		L:     &lc.LState,
		LNull: newJSONNull(&lc.LState),
	}

	return &lc
}

func (lc *LuaContext) SetupScenarioEnvironment(setIdx lua.LGFunction, finishScenario lua.LGFunction, failAssert lua.LGFunction, secrets any, env any) {
	lc.sandbox.ForEach(func(k, v lua.LValue) { lc.sandbox.RawSet(k, lua.LNil) })
	lc.sandbox.RawSetString("_G", lc.sandbox)

	lc.sinqTable = lc.NewTable()
	lc.sinqTable.RawSetString("setNextRequest", lc.NewFunction(setIdx))
	lc.sinqTable.RawSetString("finishScenario", lc.NewFunction(finishScenario))

	lc.allResponsesTable = lc.NewTable()
	lc.sinqTable.RawSetString("responses", lc.allResponsesTable)

	lc.retryTable = lc.NewTable()
	lc.retryTable.RawSetString("when", lc.NewFunctionFromProto(RetryWhen()))
	lc.retryTable.RawSetString("whenExponential", lc.NewFunctionFromProto(RetryExponential()))
	lc.retryTable.RawSetString("withJitter", lc.NewFunctionFromProto(RetryJitter()))
	lc.retryTable.RawSetString("stop", lua.LNumber(-1))

	lc.assertTable = lc.NewTable()
	lc.assertTable.RawSetString("fail", lc.NewFunction(failAssert))
	lc.assertTable.RawSetString("code", lc.NewFunctionFromProto(AssertCode()))
	lc.assertTable.RawSetString("equals", lc.NewFunctionFromProto(AssertEquals()))
	lc.assertTable.RawSetString("contains", lc.NewFunctionFromProto(AssertContains()))
	lc.assertTable.RawSetString("isTrue", lc.NewFunctionFromProto(AssertCondition()))

	lc.jsonTable = lc.NewTable()
	lc.jsonTable.RawSetString("parse", lc.NewFunction(lc.ParseJSON))
	lc.jsonTable.RawSetString("serialize", lc.NewFunction(lc.SerializeToJSON))
	lc.jsonTable.RawSetString("null", lc.parser.LNull)
	lc.sinqTable.RawSetString("json", lc.jsonTable)

	lc.cryptoTable = lc.NewTable()
	lc.cryptoTable.RawSetString("base64Encode", lc.NewFunction(Base64Encode))
	lc.cryptoTable.RawSetString("base64Decode", lc.NewFunction(Base64Decode))
	lc.cryptoTable.RawSetString("base64UrlEncode", lc.NewFunction(Base64UrlEncode))
	lc.cryptoTable.RawSetString("base64UrlDecode", lc.NewFunction(Base64UrlDecode))
	lc.cryptoTable.RawSetString("hexEncode", lc.NewFunction(HexEncode))
	lc.cryptoTable.RawSetString("hexDecode", lc.NewFunction(HexDecode))
	lc.cryptoTable.RawSetString("sha1", lc.NewFunction(SHA1Hash))
	lc.cryptoTable.RawSetString("sha256", lc.NewFunction(SHA256Hash))
	lc.cryptoTable.RawSetString("sha512", lc.NewFunction(SHA512Hash))
	lc.cryptoTable.RawSetString("hmac", lc.NewFunction(HMACHash))
	lc.sinqTable.RawSetString("crypto", lc.cryptoTable)

	lc.timeTable = lc.NewTable()
	lc.timeTable.RawSetString("ms", lua.LNumber(1))
	lc.timeTable.RawSetString("second", lua.LNumber(1000))
	lc.timeTable.RawSetString("minute", lua.LNumber(1000*60))
	lc.timeTable.RawSetString("hour", lua.LNumber(1000*60*60))
	lc.timeTable.RawSetString("now", lc.NewFunction(lc.Now))
	lc.timeTable.RawSetString("fromString", lc.NewFunction(TimeFromString))
	lc.timeTable.RawSetString("toString", lc.NewFunction(TimeToString))
	lc.sinqTable.RawSetString("time", lc.timeTable)

	lc.jwtTable = lc.NewTable()
	lc.jwtTable.RawSetString("decode", lc.NewFunction(DecodeJWT))
	lc.jwtTable.RawSetString("verify", lc.NewFunction(VerifyJWT))
	lc.jwtTable.RawSetString("sign", lc.NewFunction(SignJWT))
	lc.sinqTable.RawSetString("jwt", lc.jwtTable)

	lc.generator = *rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	lc.fakeTable = lc.NewTable()
	lc.fakeTable.RawSetString("setSeed", lc.NewFunction(lc.SetSeed))
	lc.fakeTable.RawSetString("uuid", lc.NewFunction(lc.FakeUUIDv4))
	lc.fakeTable.RawSetString("uuidv4", lc.NewFunction(lc.FakeUUIDv4))
	lc.fakeTable.RawSetString("ipv4", lc.NewFunction(lc.FakeIPv4))
	lc.fakeTable.RawSetString("ipv6", lc.NewFunction(lc.FakeIPv6))
	lc.fakeTable.RawSetString("url", lc.NewFunction(lc.FakeURL))
	lc.fakeTable.RawSetString("userAgent", lc.NewFunction(lc.FakeUserAgent))
	lc.fakeTable.RawSetString("trace", lc.NewFunction(lc.FakeTrace))
	lc.fakeTable.RawSetString("email", lc.NewFunction(lc.FakeEmail))
	lc.fakeTable.RawSetString("phone", lc.NewFunction(lc.FakePhone))
	lc.fakeTable.RawSetString("name", lc.NewFunction(lc.FakeName))
	lc.fakeTable.RawSetString("firstName", lc.NewFunction(lc.FakeFirstName))
	lc.fakeTable.RawSetString("lastName", lc.NewFunction(lc.FakeLastName))
	lc.fakeTable.RawSetString("username", lc.NewFunction(lc.FakeUsername))
	lc.fakeTable.RawSetString("password", lc.NewFunction(lc.FakePassword))
	lc.fakeTable.RawSetString("word", lc.NewFunction(lc.FakeWord))
	lc.fakeTable.RawSetString("address", lc.NewFunction(lc.FakeAddress))
	lc.fakeTable.RawSetString("company", lc.NewFunction(lc.FakeCompany))
	lc.fakeTable.RawSetString("timestamp", lc.NewFunction(lc.FakeTime))
	lc.fakeTable.RawSetString("int", lc.NewFunction(lc.FakeInt))
	lc.fakeTable.RawSetString("float", lc.NewFunction(lc.FakeFloat))
	lc.fakeTable.RawSetString("shakespeare", lc.NewFunction(lc.FakeShakespeare))
	lc.fakeTable.RawSetString("oneOf", lc.NewFunction(lc.FakeTakeOne))
	lc.sinqTable.RawSetString("fake", lc.fakeTable)

	lc.SetGlobal("secrets", ToLuaValue(secrets, &lc.LState))
	lc.SetGlobal("env", ToLuaValue(env, &lc.LState))

	lc.SetGlobal("sinq", lc.sinqTable)
}

func (lc *LuaContext) SetupRequestEnvironment(requestIdx int) {
	lc.RequestTable = lc.NewTable()
	lc.ResponseTable = lc.NewTable()
	lc.allResponsesTable.RawSetInt(requestIdx+1, lc.ResponseTable)
	lc.SetGlobal("req", lc.RequestTable)
	lc.SetGlobal("res", lc.ResponseTable)
}

func (lc *LuaContext) RunSandboxed(byteCode *lua.FunctionProto, timeout time.Duration) error {
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

func (lc *LuaContext) RecordResponseMeta(attempt int, code int, headers http.Header) {
	lc.ResponseTable.RawSetString("attempt", lua.LNumber(attempt))
	lc.ResponseTable.RawSetString("code", lua.LNumber(code))
	lc.ResponseTable.RawSetString("headers", ToLuaValue(headers, &lc.LState))
}

func (lc *LuaContext) RecordResponseFile(written int64) {
	lc.ResponseTable.RawSetString("size", lua.LNumber(written))
}

func (lc *LuaContext) RecordResponseBody(data []byte, oversized bool) {
	if oversized {
		lc.ResponseTable.RawSetString("oversized", lua.LTrue)
	} else {
		lc.ResponseTable.RawSetString("oversized", lua.LFalse)
	}

	lc.ResponseTable.RawSetString("bodyRaw", lua.LString(data))
	lc.ResponseTable.RawSetString("bodyJson", lua.LNil)

	lc.ResponseTable.RawSetString("extractBodyJson", lc.NewClosure(lc.ExtractBodyJson, lc.ResponseTable))
	lc.ResponseTable.RawSetString("json", lc.NewClosure(lc.ExtractBodyJsonUnsafe, lc.ResponseTable))
}

func (lc *LuaContext) SetupPreScript(attach lua.LGFunction, saveTo lua.LGFunction, cache lua.LGFunction, skip lua.LGFunction) {
	lc.RequestTable.RawSetString("attach", lc.NewFunction(attach))
	lc.RequestTable.RawSetString("saveResponseTo", lc.NewFunction(saveTo))
	lc.RequestTable.RawSetString("cache", lc.NewFunction(cache))
	lc.RequestTable.RawSetString("skip", lc.NewFunction(skip))
}

func (lc *LuaContext) TearDownPreScript() {
	lc.RequestTable.RawSetString("attach", lua.LNil)
	lc.RequestTable.RawSetString("saveResponseTo", lua.LNil)
	lc.RequestTable.RawSetString("cache", lua.LNil)
	lc.RequestTable.RawSetString("skip", lua.LNil)
}

func (lc *LuaContext) SetupRetryScript() {
	lc.sinqTable.RawSetString("retry", lc.retryTable)
}

func (lc *LuaContext) TearDownRetryScript() {
	lc.sinqTable.RawSetString("retry", lua.LNil)
}

func (lc *LuaContext) SetupAssertScript(fileMatches lua.LGFunction) {
	lc.assertTable.RawSetString("fileMatches", lc.NewFunction(fileMatches))
	lc.sinqTable.RawSetString("assert", lc.assertTable)
}

func (lc *LuaContext) TearDownAssertScript() {
	lc.assertTable.RawSetString("fileMatches", lua.LNil)
	lc.sinqTable.RawSetString("assert", lua.LNil)
}
