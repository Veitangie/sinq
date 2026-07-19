// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/Veitangie/sinq/internal/luapi"
	"github.com/Veitangie/sinq/internal/scenario"
	"github.com/Veitangie/sinq/internal/timer"
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

func (w *worker) runPreScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (string, string, bool, bool, error) {
	var filenameIn, filenameOut string
	var cache, skip bool

	w.lc.SetupPreScript(func(ls *lua.LState) int {
		attachedPath := ls.CheckString(1)
		if filepath.IsAbs(attachedPath) {
			ls.RaiseError("req.attach: invalid file path '%s', should not be absolute", attachedPath)
			return 0
		}

		resolvedPath := filepath.Join(filepath.Dir(filename), attachedPath)

		info, err := w.env.workspace.Stat(resolvedPath)
		if err != nil || info.IsDir() {
			ls.RaiseError("req.attach: invalid file path '%s' (resolved as '%s')", attachedPath, resolvedPath)
			return 0
		}
		filenameIn = resolvedPath
		return 0
	}, func(ls *lua.LState) int {
		savePath := ls.CheckString(1)
		if filepath.IsAbs(savePath) {
			ls.RaiseError("req.saveResponseTo: invalid file path '%s', should not be absolute", savePath)
			return 0
		}

		resolvedPath := filepath.Join(filepath.Dir(filename), savePath)

		info, err := w.env.workspace.Stat(filepath.Dir(resolvedPath))
		if err != nil || !info.IsDir() {
			ls.RaiseError("req.saveResponseTo: invalid file path '%s' (resolved path '%s' does not exist or is not a directory)", savePath, filepath.Dir(resolvedPath))
			return 0
		}
		filenameOut = resolvedPath
		return 0
	},
		func(ls *lua.LState) int {
			flag := ls.OptBool(1, true)
			cache = flag
			return 0
		},
		func(ls *lua.LState) int {
			flag := ls.OptBool(1, true)

			skip = flag
			return 0
		},
	)
	defer w.lc.TearDownPreScript()

	err := w.runEffectfulScript(token, extract, filename, executionTimeout)

	return filenameIn, filenameOut, cache, skip, err
}

func (w *worker) runRetryScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (int64, error) {
	if token.Type != scenario.Script {
		return -1, nil
	}

	w.lc.SetupRetryScript()
	defer w.lc.TearDownRetryScript()

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

func (w *worker) compareFiles(filename, saveToFilename string) lua.LGFunction {
	return func(ls *lua.LState) int {
		otherFilename := ls.CheckString(1)
		if saveToFilename == "" {
			ls.Error(lua.LString("sinq.assert.fileMatches called on request which was not saved to file"), 1)
			return 0
		}

		requestFile, err := w.env.workspace.Open(saveToFilename)
		if err != nil {
			ls.Error(lua.LString(fmt.Sprintf("sinq.assert.fileMatches failed to open saved response: %s", err.Error())), 1)
			return 0
		}

		otherFile, err := w.env.workspace.Open(filepath.Join(filepath.Dir(filename), otherFilename))
		if err != nil {
			requestFile.Close()
			ls.Error(lua.LString(fmt.Sprintf("sinq.assert.fileMatches failed to open file: %s", err.Error())), 1)
			return 0
		}

		defer func() {
			err := recover()
			otherFile.Close()
			requestFile.Close()
			if err != nil {
				panic(err)
			}
		}()

		fromReq := make([]byte, 32*1<<10)
		fromFile := make([]byte, 32*1<<10)
		totalReadReq, totalReadFile := 0, 0
		for {
			if ls.Context().Err() != nil {
				return 0
			}

			readReq, errReq := io.ReadFull(requestFile, fromReq)
			if errReq != nil && errReq != io.ErrUnexpectedEOF && errReq != io.EOF {
				ls.Error(lua.LString(fmt.Sprintf("sinq.assert.fileMatches failed to read from saved response: %s", errReq.Error())), 1)
				return 0
			}

			totalReadReq += readReq

			if ls.Context().Err() != nil {
				return 0
			}

			readFile, errFile := io.ReadFull(otherFile, fromFile)
			if errFile != nil && errFile != io.ErrUnexpectedEOF && errFile != io.EOF {
				ls.Error(lua.LString(fmt.Sprintf("sinq.assert.fileMatches failed to read from file: %s", errFile.Error())), 1)
				return 0
			}

			if ls.Context().Err() != nil {
				return 0
			}

			totalReadFile += readFile

			if errReq == io.EOF && errFile == io.EOF {
				break
			}

			if (errReq == io.EOF && errFile != io.EOF) ||
				(errReq != io.EOF && errFile == io.EOF) ||
				totalReadReq != totalReadFile ||
				!bytes.Equal(fromReq[:readReq], fromFile[:readFile]) {

				w.assertionFailures = append(w.assertionFailures, "Saved response and file did not match")
				break
			}

		}

		return 0
	}
}

func (w *worker) runAssertScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration, saveToFilename string) error {
	w.lc.SetupAssertScript(w.compareFiles(filename, saveToFilename))
	defer w.lc.TearDownAssertScript()

	return w.runEffectfulScript(token, extract, filename, executionTimeout)
}

func (w *worker) safeExecute(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) error {
	byteCode, err := w.env.compiler.compileScript(token, extract, filename)
	if err != nil {
		return err
	}

	return w.lc.RunSandboxed(byteCode, executionTimeout)
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

func (w *worker) captureBodyToFile(body io.Reader, filenameTo string) (int64, error) {
	file, err := w.env.workspace.Create(filenameTo)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	return io.Copy(file, body)
}

func (w *worker) requestCompleted(response intermediate) (string, error) {
	w.lc.RecordResponseMeta(response.attempt, response.statusCode, response.headers)

	var err error
	data := []byte(response.filenameTo)
	if response.filenameTo != "" {
		w.lc.RecordResponseFile(response.size)
	} else {
		w.lc.RecordResponseBody(response.body, response.oversized)
		data = response.body
	}

	var result string
	if w.env.cfg.DumpOnFailure {
		sb := strings.Builder{}
		sb.WriteString(response.status)
		sb.WriteByte(' ')
		sb.WriteString(response.proto)
		sb.WriteByte('\n')
		for key, values := range response.headers {
			sb.WriteString(key)
			sb.Write([]byte(": "))
			for idx, value := range values {
				sb.WriteString(value)
				if idx != len(values)-1 {
					sb.Write([]byte(", "))
				}
			}
			sb.WriteByte('\n')
		}
		sb.WriteByte('\n')
		sb.Write(data)
		result = sb.String()
	}

	return result, err
}

func (w *worker) setupScenarioEnvironment(ctx context.Context, env map[string]any) error {
	if w.env.cfg.Safe || w.lc == nil {
		if w.lc != nil {
			w.lc.Close()
		}
		w.lc = luapi.NewLuaContext(timer.DefaultClock{}, w.env.cfg.Unrestricted, w.env.luaPath)
	}

	w.lc.SetContext(ctx)

	w.lc.SetupScenarioEnvironment(w.setRequestIdx, w.finishScenario, w.failAssert, w.env.secrets, env)

	return nil
}
