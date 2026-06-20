// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"sync"

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

type cachedCompiler struct {
	scriptCacheLock *sync.RWMutex
	scriptCache     map[scriptKey]*lua.FunctionProto
}

func (cc cachedCompiler) compileScript(token scenario.Token, extract extractPayloadFunc, filename string) (*lua.FunctionProto, error) {
	scriptName := scriptKey{filename, token.Name, token.Start}

	cc.scriptCacheLock.RLock()
	if cache, ok := cc.scriptCache[scriptName]; ok {
		cc.scriptCacheLock.RUnlock()
		return cache, nil
	}
	cc.scriptCacheLock.RUnlock()

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

	cc.scriptCacheLock.Lock()
	if cache, ok := cc.scriptCache[scriptName]; ok {
		cc.scriptCacheLock.Unlock()
		return cache, nil
	}
	cc.scriptCache[scriptName] = compiled
	cc.scriptCacheLock.Unlock()

	return compiled, nil
}
