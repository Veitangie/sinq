// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

type extractPayloadFunc = func(scenario.Token) []byte

type scriptKey struct {
	filename   string
	scriptname string
	length     int
}

func (sk scriptKey) string() string {
	return sk.filename + "#" + sk.scriptname
}

func (w *worker) setRequestIdx(ls *lua.LState) int {
	top := ls.GetTop()
	if top <= 0 {
		ls.Error(lua.LString("No value provided for the next request index"), 1)
		return 0
	}

	nextIdxRaw := ls.Get(-1)
	ls.Pop(1)

	nextIdxFloat, ok := nextIdxRaw.(lua.LNumber)
	if !ok {
		ls.Error(lua.LString("The value provided for the next request index is not a number"), 1)
		return 0
	}

	nextIdx := int(nextIdxFloat)
	nextIdx--
	if nextIdx >= 0 && nextIdx <= w.maxRequestIdx {
		w.requestIdx = nextIdx
	}

	return 0
}

func (w *worker) failAssert(ls *lua.LState) int {
	top := ls.GetTop()
	if top <= 0 {
		ls.Error(lua.LString("No reason provided for assertion failure"), 1)
		return 0
	}

	reasonRaw := ls.Get(-1)
	ls.Pop(1)

	reasonString := reasonRaw.String()
	w.assertionFailures = append(w.assertionFailures, reasonString)
	return 0
}

func (w *worker) runEffectfulScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) error {
	return w.safeExecute(token, extract, filename, executionTimeout)
}

func (w *worker) runRetryScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (int64, error) {
	if token.Type != scenario.Script {
		return -1, nil
	}

	shouldBeNumber, err := w.executeAndExtractValue(token, extract, filename, executionTimeout)
	if err != nil {
		return -1, err
	}

	if shouldBeNumber.Type() != lua.LTNumber {
		return -1, fmt.Errorf("%s#%s: Expecting the return value of script to be number, got %s instead", filename, token.Name, shouldBeNumber.Type().String())
	}

	resNumber, ok := shouldBeNumber.(lua.LNumber)
	if !ok {
		return -1, fmt.Errorf("%s#%s: Failed to cast the result to number", filename, token.Name)
	}

	return int64(resNumber), nil
}

func (w *worker) compileScript(token scenario.Token, extract extractPayloadFunc, filename string) (*lua.FunctionProto, error) {
	scriptName := scriptKey{filename, token.Name, token.Start}

	w.scriptCacheLock.RLock()
	if cache, ok := w.scriptCache[scriptName]; ok {
		w.scriptCacheLock.RUnlock()
		return cache, nil
	}
	w.scriptCacheLock.RUnlock()

	script := extract(token)
	buf := make([]byte, 0, token.Line+token.Offset+len(token.Name)+2+len(script))
	for range token.Line - 1 {
		buf = append(buf, '\n')
	}
	for range token.Offset + len(token.Name) + 1 {
		buf = append(buf, ' ')
	}

	buf = append(buf, script...)
	scriptNameString := scriptName.string()
	chunk, err := parse.Parse(bytes.NewReader(buf), scriptNameString)
	if err != nil {
		return nil, err
	}
	compiled, err := lua.Compile(chunk, scriptNameString)
	if err != nil {
		return nil, err
	}

	w.scriptCacheLock.Lock()
	if cache, ok := w.scriptCache[scriptName]; ok {
		w.scriptCacheLock.Unlock()
		return cache, nil
	}
	w.scriptCache[scriptName] = compiled
	w.scriptCacheLock.Unlock()

	return compiled, nil
}

func (w *worker) safeExecute(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) error {
	byteCode, err := w.compileScript(token, extract, filename)
	if err != nil {
		return err
	}

	fn := w.ls.NewFunctionFromProto(byteCode)
	fn.Env = w.sandbox

	w.ls.Push(fn)

	oldCtx := w.ls.Context()
	if oldCtx == nil {
		oldCtx = context.Background()
	}
	ctxWithTimeout, cancelCtx := context.WithTimeout(oldCtx, executionTimeout)
	defer cancelCtx()

	w.ls.SetContext(ctxWithTimeout)
	err = w.ls.PCall(0, lua.MultRet, nil)
	w.ls.SetContext(oldCtx)
	return err
}

func fixError(err error) error {
	switch errTyped := err.(type) {
	case *parse.Error:
		return nil
	case *lua.ApiError:
		if errTyped.Type == lua.ApiErrorSyntax {
			return nil
		}
		return err
	default:
		return err
	}
}

func (w *worker) executeAndExtractValue(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (lua.LValue, error) {

	oldTop := w.ls.GetTop()
	defer w.ls.SetTop(oldTop)

	err := w.safeExecute(token, extract, filename, executionTimeout)
	newTop := w.ls.GetTop()
	if err == nil {
		diff := newTop - oldTop
		if diff < 1 {
			return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced no value", filename, token.Name, token.Line, token.Offset)
		}
		value := w.ls.Get(-1)
		return value, nil
	}

	fixed := fixError(err)
	if fixed != nil {
		return nil, fmt.Errorf("Error occurred while executing lua script: %w", err)
	}

	oldTop = newTop
	extractWithReturn := func(token scenario.Token) []byte {
		return append([]byte("return "), extract(token)...)
	}
	err2 := w.safeExecute(token, extractWithReturn, filename, executionTimeout)
	newTop = w.ls.GetTop()
	if err2 != nil {
		return nil, fmt.Errorf("Failed to execute lua script: %w", err)
	}

	diff := newTop - oldTop
	if diff < 1 {
		return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced no value", filename, token.Name, token.Line, token.Offset)
	}
	value := w.ls.Get(-1)
	return value, nil
}

func (w *worker) toLuaValue(value any) lua.LValue {
	if value == nil {
		return lua.LNil
	}

	switch v := value.(type) {
	case bool:
		return lua.LBool(v)

	case float64:
		return lua.LNumber(v)

	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)

	case string:
		return lua.LString(v)

	case []any:
		tbl := w.ls.NewTable()
		for _, item := range v {
			tbl.Append(w.toLuaValue(item))
		}
		return tbl

	case map[string]any:
		tbl := w.ls.NewTable()
		for key, val := range v {
			tbl.RawSetString(key, w.toLuaValue(val))
		}
		return tbl

	case http.Header:
		tbl := w.ls.NewTable()
		for key, values := range v {
			if len(values) == 1 {
				tbl.RawSetString(key, lua.LString(values[0]))
			} else if len(values) > 1 {
				valArray := w.ls.NewTable()
				for _, val := range values {
					valArray.Append(lua.LString(val))
				}
				tbl.RawSetString(key, valArray)
			} else {
				tbl.RawSetString(key, lua.LString(""))
			}
		}
		return tbl

	default:
		return lua.LNil
	}
}

func (w *worker) requestCompleted(ctx context.Context, response *http.Response, requestIdx int) error {
	defer response.Body.Close()

	resTable := w.ls.NewTable()
	resTable.RawSetString("code", lua.LNumber(response.StatusCode))
	resTable.RawSetString("headers", w.toLuaValue(response.Header))

	data, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if w.logger.Enabled(ctx, slog.LevelDebug) {
		w.logger.Debug("Extracted response", append(w.loggingContext(ctx), "code", response.StatusCode, "headers", response.Header, "body", string(data))...)
	}

	resTable.RawSetString("bodyRaw", lua.LString(data))

	var unmarshalledResult any
	err = json.Unmarshal(data, &unmarshalledResult)
	if err == nil {
		resTable.RawSetString("body", w.toLuaValue(unmarshalledResult))
	}

	// lua indexes start from 1
	w.responsesTable.RawSetInt(requestIdx+1, resTable)

	return nil
}

func (w *worker) setupLuaEnvironment(ctx context.Context, env map[string]any) error {
	if w.luaStateHardReset || w.ls == nil {
		if w.ls != nil {
			w.ls.Close()
		}
		w.ls = lua.NewState()
		w.ls.SetContext(ctx)

		w.sandbox = w.ls.NewTable()
		sandboxMeta := w.ls.NewTable()
		w.ls.SetField(sandboxMeta, "__index", w.ls.Get(lua.GlobalsIndex))
		w.sandbox.Metatable = sandboxMeta
	}

	w.sandbox.ForEach(func(k, v lua.LValue) { w.sandbox.RawSet(k, lua.LNil) })

	sinqTable := w.ls.NewTable()
	sinqTable.RawSetString("setNextRequest", w.ls.NewFunction(w.setRequestIdx))

	testTable := w.ls.NewTable()
	testTable.RawSetString("fail", w.ls.NewFunction(w.failAssert))
	sinqTable.RawSetString("test", testTable)

	responsesTable := w.ls.NewTable()
	w.responsesTable = responsesTable
	sinqTable.RawSetString("responses", responsesTable)

	w.ls.SetGlobal("secrets", w.toLuaValue(w.secrets))
	w.ls.SetGlobal("env", w.toLuaValue(env))

	w.ls.SetGlobal("sinq", sinqTable)
	return nil
}
