// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

func (w *worker) setRequestIdx(ls *lua.LState) int {
	nextIdxFloat := ls.CheckNumber(1)

	nextIdx := int(nextIdxFloat) - 1
	if nextIdx >= 0 && nextIdx <= w.maxRequestIdx {
		w.requestIdx = nextIdx
	}
	return 0
}

func (w *worker) finishScenario(ls *lua.LState) int {
	w.requestIdx = w.maxRequestIdx + 1
	return 0
}

func (w *worker) failAssert(ls *lua.LState) int {
	reasonString := ls.CheckString(1)
	w.assertionFailures = append(w.assertionFailures, reasonString)
	return 0
}

func (w *worker) runEffectfulScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) error {
	return w.safeExecute(token, extract, filename, executionTimeout)
}

func (w *worker) runPreScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (string, string, error) {
	var filenameIn, filenameOut string

	w.lc.setupPreScript(func(L *lua.LState) int {
		attachedPath := L.CheckString(1)
		if filepath.IsAbs(attachedPath) {
			L.RaiseError("req.attach: invalid file path '%s', should not be absolute", attachedPath)
			return 0
		}

		resolvedPath := filepath.Join(filepath.Dir(filename), attachedPath)

		info, err := w.env.workspace.Stat(resolvedPath)
		if err != nil || info.IsDir() {
			L.RaiseError("req.attach: invalid file path '%s' (resolved as '%s')", attachedPath, resolvedPath)
			return 0
		}
		filenameIn = resolvedPath
		return 0
	}, func(L *lua.LState) int {
		savePath := L.CheckString(1)
		if filepath.IsAbs(savePath) {
			L.RaiseError("req.saveResponseTo: invalid file path '%s', should not be absolute", savePath)
			return 0
		}

		resolvedPath := filepath.Join(filepath.Dir(filename), savePath)

		info, err := w.env.workspace.Stat(filepath.Dir(resolvedPath))
		if err != nil || !info.IsDir() {
			L.RaiseError("req.saveResponseTo: invalid file path '%s' (resolved path '%s' does not exist or is not a directory)", savePath, filepath.Dir(resolvedPath))
			return 0
		}
		filenameOut = resolvedPath
		return 0
	})
	defer w.lc.tearDownPreScript()

	err := w.runEffectfulScript(token, extract, filename, executionTimeout)

	return filenameIn, filenameOut, err
}

func (w *worker) runRetryScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (int64, error) {
	if token.Type != scenario.Script {
		return -1, nil
	}

	w.lc.setupRetryScript()
	defer w.lc.tearDownRetryScript()

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

func (w *worker) runAssertScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) error {
	w.lc.setupAssertScript()
	defer w.lc.tearDownAssertScript()

	return w.runEffectfulScript(token, extract, filename, executionTimeout)
}

func (w *worker) safeExecute(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) error {
	byteCode, err := w.env.compiler.compileScript(token, extract, filename)
	if err != nil {
		return err
	}

	return w.lc.runSandboxed(byteCode, executionTimeout)
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

	oldTop := w.lc.GetTop()
	defer w.lc.SetTop(oldTop)

	err := w.safeExecute(token, extract, filename, executionTimeout)
	newTop := w.lc.GetTop()
	if err == nil {
		diff := newTop - oldTop
		if diff < 1 {
			return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced no value", filename, token.Name, token.Line, token.Offset)
		}
		value := w.lc.Get(-1)
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
	newTop = w.lc.GetTop()
	if err2 != nil {
		return nil, fmt.Errorf("Failed to execute lua script: %w", err)
	}

	diff := newTop - oldTop
	if diff < 1 {
		return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced no value", filename, token.Name, token.Line, token.Offset)
	}
	value := w.lc.Get(-1)
	return value, nil
}

func (w *worker) captureBodyToFile(body io.Reader, filenameTo string) error {
	file, err := w.env.workspace.Create(filenameTo)
	if err != nil {
		return err
	}
	defer file.Close()

	written, err := io.Copy(file, body)
	if err != nil {
		return err
	}

	w.lc.RecordResponseFile(written)
	return nil
}

func (w *worker) captureBodyToLua(body io.Reader, maxBodySize uint64) ([]byte, error) {
	limited := io.LimitReader(body, int64(maxBodySize)+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return data, err
	}

	oversized := false
	if uint64(len(data)) > maxBodySize {
		data = data[:len(data)-1]
		io.Copy(io.Discard, body)
		oversized = true
	}

	w.lc.RecordResponseBody(data, oversized)
	return data, nil
}

func (w *worker) requestCompleted(ctx context.Context, response *http.Response, filenameTo string, maxBodySize uint64, attempt int) error {
	defer response.Body.Close()

	w.lc.RecordResponseMeta(attempt, response.StatusCode, response.Header)

	var err error
	if filenameTo != "" {
		err = w.captureBodyToFile(response.Body, filenameTo)
	} else {
		var data []byte
		data, err = w.captureBodyToLua(response.Body, maxBodySize)
		if w.env.logger.Enabled(ctx, slog.LevelDebug) {
			w.env.logger.Debug("[Runner] Extracted response", append(w.loggingContext(ctx), "code", response.StatusCode, "headers", response.Header, "body", string(data))...)
		}
	}

	return err
}

func (w *worker) setupScenarioEnvironment(ctx context.Context, env map[string]any) error {
	if w.env.luaStateHardReset || w.lc == nil {
		if w.lc != nil {
			w.lc.Close()
		}
		w.lc = newLuaContext()
	}

	w.lc.SetContext(ctx)

	w.lc.setupScenarioEnvironment(w.setRequestIdx, w.finishScenario, w.failAssert, w.env.secrets, env)

	return nil
}
