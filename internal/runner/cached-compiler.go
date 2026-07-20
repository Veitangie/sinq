// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"errors"
	"hash/maphash"
	"strconv"
	"sync"

	"github.com/Veitangie/sinq/internal/scenario"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"golang.org/x/sync/singleflight"
)

type extractPayloadFunc = func(scenario.Token) []byte

type scriptKey struct {
	filename   string
	scriptname string
	offset     int
}

func (sk scriptKey) string() string {
	return sk.filename + "#" + sk.scriptname
}

func (sk scriptKey) stringKey() string {
	return sk.filename + "/" + sk.scriptname + "/" + strconv.Itoa(sk.offset)
}

type cachedCompiler struct {
	group      singleflight.Group
	cache      sync.Map
	hasherSeed maphash.Seed
}

func (cc *cachedCompiler) compileScript(token scenario.Token, extract extractPayloadFunc, filename string) (*lua.FunctionProto, error) {
	scriptName := scriptKey{filename, token.Name, token.Start}

	script := extract(token)
	hasher := maphash.Hash{}
	hasher.SetSeed(cc.hasherSeed)
	hasher.WriteString(scriptName.stringKey())
	hasher.Write(script)
	hash := strconv.FormatUint(hasher.Sum64(), 16)

	if cache, ok := cc.cache.Load(hash); ok {
		if cacheTyped, ok := cache.(*lua.FunctionProto); ok {
			return cacheTyped, nil
		}
	}

	res, err, _ := cc.group.Do(hash, func() (any, error) {
		if cache, ok := cc.cache.Load(hash); ok {
			if cacheTyped, ok := cache.(*lua.FunctionProto); ok {
				return cacheTyped, nil
			}
		}

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

		cc.cache.Store(hash, compiled)

		return compiled, nil
	})
	if err != nil {
		return nil, err
	}

	if resTyped, ok := res.(*lua.FunctionProto); ok {
		return resTyped, nil
	}
	return nil, errors.New("Failed to compile")
}
