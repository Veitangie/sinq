// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"veitangie.dev/sinq/internal/luapi"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
)

type preScriptResultBuilder struct {
	workspace Workspace
	filename  string
	res       preScriptResult
}

type preScriptResult struct {
	multipart   multipartData
	filenameIn  string
	filenameOut string
	cacheKey    string
	cache       bool
	skip        bool
}

func (b *preScriptResultBuilder) attach() lua.LGFunction {
	return func(ls *lua.LState) int {
		attachedPath := ls.CheckString(1)
		if filepath.IsAbs(attachedPath) {
			ls.RaiseError("req.attach: invalid file path '%s', should not be absolute", attachedPath)
			return 0
		}

		resolvedPath := filepath.Join(filepath.Dir(b.filename), attachedPath)

		info, err := b.workspace.Stat(resolvedPath)
		if err != nil || info.IsDir() {
			ls.RaiseError("req.attach: invalid file path '%s' (resolved as '%s')", attachedPath, resolvedPath)
			return 0
		}
		b.res.filenameIn = resolvedPath
		return 0
	}
}

func (b *preScriptResultBuilder) saveTo() lua.LGFunction {
	return func(ls *lua.LState) int {
		savePath := ls.CheckString(1)
		if filepath.IsAbs(savePath) {
			ls.RaiseError("req.saveResponseTo: invalid file path '%s', should not be absolute", savePath)
			return 0
		}

		resolvedPath := filepath.Join(filepath.Dir(b.filename), savePath)
		resolvedPathDir := filepath.Dir(resolvedPath)

		info, err := b.workspace.Stat(resolvedPathDir)
		if os.IsNotExist(err) {
			err = b.workspace.MkDirall(resolvedPathDir)
			if err != nil {
				ls.RaiseError("req.saveResponseTo: invalid file path '%s' (failed to create directory at %s)", savePath, resolvedPathDir)
				return 0
			}
			info, err = b.workspace.Stat(resolvedPathDir)
		}

		if err != nil || !info.IsDir() {
			ls.RaiseError("req.saveResponseTo: invalid file path '%s' (resolved path '%s' is not a directory)", savePath, resolvedPathDir)
			return 0
		}
		b.res.filenameOut = resolvedPath
		return 0
	}
}

func (b *preScriptResultBuilder) cache() lua.LGFunction {
	return func(ls *lua.LState) int {
		flag := true
		key := ""
		switch typed := ls.Get(1).(type) {
		case lua.LBool:
			flag = bool(typed)
		case lua.LString:
			key = string(typed)
		default:
			if ls.GetTop() != 0 {
				ls.RaiseError("req.cache: Unexpected argument type %s, expected string or bool", ls.Get(1).Type().String())
				return 0
			}
		}
		b.res.cacheKey = key
		b.res.cache = flag
		return 0
	}
}

func (b *preScriptResultBuilder) skip() lua.LGFunction {
	return func(ls *lua.LState) int {
		flag := ls.OptBool(1, true)

		b.res.skip = flag
		return 0
	}
}

func lTableToHeaders(value *lua.LTable) (map[string][]string, error) {
	res := make(map[string][]string, value.Len())
	var err error
	value.ForEach(func(key, value lua.LValue) {
		if err != nil {
			return
		}

		keyString, ok := key.(lua.LString)
		if !ok {
			err = errors.New("header key must be a string")
			return
		}
		actualKey := textproto.CanonicalMIMEHeaderKey(string(keyString))

		existing := res[actualKey]
		switch typed := value.(type) {
		case lua.LString:
			res[actualKey] = append(existing, string(typed))
		case lua.LNumber:
			res[actualKey] = append(existing, typed.String())
		case lua.LBool:
			res[actualKey] = append(existing, typed.String())
		default:
			err = errors.New("header value must be a string, number or bool")
		}
	})
	return res, err
}

func stringOrNil(tbl *lua.LTable, key string) (string, error) {
	value := tbl.RawGetString(key)
	switch typed := value.(type) {
	case *lua.LNilType:
		return "", nil
	case lua.LString:
		return string(typed), nil
	default:
		return "", fmt.Errorf("expected %s to be string, got %s", key, value.Type().String())
	}
}

func allArgs(ls *lua.LState) []*lua.LTable {
	top := ls.GetTop()
	args := make([]*lua.LTable, 0, top)
	for i := 1; i <= top; i++ {
		args = append(args, ls.CheckTable(i))
	}

	if len(args) == 1 && args[0].Len() != 0 {
		tbl := args[0]
		newArgs := make([]*lua.LTable, 0, tbl.Len())
		for i := 1; i <= tbl.MaxN(); i++ {
			if next, ok := tbl.RawGetInt(i).(*lua.LTable); ok {
				newArgs = append(newArgs, next)
			} else {
				break
			}
		}

		if len(newArgs) == tbl.Len() {
			args = newArgs
		}
	}

	return args
}

func contentDispositionAndTypeFromTable(tbl *lua.LTable, source string) (string, string, error) {
	var contentDisposition, contentType string
	var err error
	if contentDisposition, err = stringOrNil(tbl, "contentDisposition"); err != nil {
		return "", "", err
	}
	if contentType, err = stringOrNil(tbl, "contentType"); err != nil {
		return "", "", err
	}
	if contentDisposition == "" {
		var name, filename string
		if name, err = stringOrNil(tbl, "name"); err != nil {
			return "", "", err
		}
		if name == "" {
			return "", "", errors.New("entry has neither contentDisposition nor name")
		}

		if useSource, ok := tbl.RawGetString("filename").(lua.LBool); ok {
			if bool(useSource) && source != "" {
				filename = filepath.Base(source)
			}
		} else if filename, err = stringOrNil(tbl, "filename"); err != nil {
			return "", "", err
		}

		trailer := ""
		if filename != "" {
			trailer = fmt.Sprintf("; filename=\"%s\"", filename)
			if contentType == "" {
				contentType = contentTypeFromFile(filepath.Base(filename))
			}
			if contentType == "" {
				contentType = contentTypeFromFile(filepath.Base(source))
			}
			if contentType == "" {
				contentType = octetStream
			}
		}
		contentDisposition = fmt.Sprintf("form-data; name=\"%s\"%s", name, trailer)
	}
	return contentDisposition, contentType, nil
}

func (b *preScriptResultBuilder) multipart() lua.LGFunction {
	return func(ls *lua.LState) int {
		args := allArgs(ls)
		acc := make([]multipartEntry, 0, len(args))
		for _, tbl := range args {
			headers := make(map[string][]string)
			if headersTable, ok := tbl.RawGetString("headers").(*lua.LTable); ok {
				var err error
				headers, err = lTableToHeaders(headersTable)
				if err != nil {
					ls.RaiseError("req.multipart: invalid headers: %s", err.Error())
					return 0
				}
			}

			var data, source, contentDisposition, contentType string
			var err error
			if data, err = stringOrNil(tbl, "data"); err != nil {
				ls.RaiseError("req.multipart: %s", err.Error())
				return 0
			}
			if source, err = stringOrNil(tbl, "source"); err != nil {
				ls.RaiseError("req.multipart: %s", err.Error())
				return 0
			}
			if data != "" && source != "" {
				ls.RaiseError("req.multipart: entry has both data and source")
				return 0
			}

			if source != "" {
				if filepath.IsAbs(source) {
					ls.RaiseError("req.multipart: invalid source path '%s', should not be absolute", source)
					return 0
				}

				resolvedPath := filepath.Join(filepath.Dir(b.filename), source)

				info, err := b.workspace.Stat(resolvedPath)
				if err != nil || info.IsDir() {
					ls.RaiseError("req.multipart: invalid source path '%s' (resolved as '%s')", source, resolvedPath)
					return 0
				}
				source = resolvedPath
			}

			contentDisposition, contentType, err = contentDispositionAndTypeFromTable(tbl, source)
			if err != nil {
				ls.RaiseError("req.multipart: %s", err.Error())
				return 0
			}

			acc = append(acc, multipartEntry{
				data:               data,
				source:             source,
				contentDisposition: contentDisposition,
				contentType:        contentType,
				headers:            headers,
			})
		}
		b.res.multipart.entries = append(b.res.multipart.entries, acc...)

		return 0
	}
}

func (b *preScriptResultBuilder) multipartBoundary() lua.LGFunction {
	return func(ls *lua.LState) int {
		boundary := ls.CheckString(1)
		b.res.multipart.boundary = boundary
		return 0
	}
}

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
	if token.Type == scenario.IncompleteToken {
		return nil
	}
	return w.safeExecute(token, extract, filename, executionTimeout)
}

func (w *worker) runPreScript(token scenario.Token, extract extractPayloadFunc, filename string, executionTimeout time.Duration) (preScriptResult, error) {
	resBuilder := preScriptResultBuilder{workspace: w.env.workspace, filename: filename}

	w.lc.SetupPreScript(
		resBuilder.attach(),
		resBuilder.saveTo(),
		resBuilder.cache(),
		resBuilder.skip(),
		resBuilder.multipart(),
		resBuilder.multipartBoundary(),
	)
	defer w.lc.TearDownPreScript()

	err := w.runEffectfulScript(token, extract, filename, executionTimeout)

	return resBuilder.res, err
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
	valueIdx := oldTop + 1
	defer w.lc.SetTop(oldTop)

	err := w.safeExecute(token, extract, filename, executionTimeout)
	newTop := w.lc.GetTop()
	if err == nil {
		diff := newTop - oldTop
		if diff < 1 {
			return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced no value", filename, token.Name, token.Line, token.Offset)
		}
		value := w.lc.Get(valueIdx)
		if value == lua.LNil {
			return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced nil value", filename, token.Name, token.Line, token.Offset)
		}
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
	value := w.lc.Get(valueIdx)
	if value == lua.LNil {
		return nil, fmt.Errorf("%s#%s:%d:%d: Lua script didn't fail in execution but produced nil value", filename, token.Name, token.Line, token.Offset)
	}
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
		if len(data) > 0 && data[len(data)-1] != '\n' {
			sb.WriteByte('\n')
		}
		result = sb.String()
	}

	return result, err
}

func (w *worker) setupScenarioEnvironment(ctx context.Context, env map[string]any) error {
	if w.lc != nil {
		w.lc.Close()
	}
	var printer *bytes.Buffer
	if w.env.cfg.Print {
		printer = &bytes.Buffer{}
	}
	w.lc = luapi.NewLuaContext(timer.DefaultClock{}, w.env.cfg.Unrestricted, w.env.loader.load, printer)

	w.lc.SetContext(ctx)

	w.lc.SetupScenarioEnvironment(w.setRequestIdx, w.finishScenario, w.failAssert, w.env.secrets, env)

	return nil
}
