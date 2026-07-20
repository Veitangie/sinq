// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
	"golang.org/x/sync/singleflight"
)

type cachedResult[T any] struct {
	res T
	err error
}

type cachedLoader struct {
	path      []string
	workspace Workspace
	group     singleflight.Group
	cache     sync.Map
}

func (c *cachedLoader) load(ls *lua.LState) int {
	moduleName := ls.CheckString(1)
	if res, ok := c.cache.Load(moduleName); ok {
		if cachedRes, ok := res.(cachedResult[*lua.FunctionProto]); ok {
			if cachedRes.err != nil {
				ls.Push(lua.LString(cachedRes.err.Error()))
			} else {
				ls.Push(ls.NewFunctionFromProto(cachedRes.res))
			}
			return 1
		}
	}

	select {
	case res := <-c.group.DoChan(moduleName, func() (any, error) {
		if res, ok := c.cache.Load(moduleName); ok {
			if cachedRes, ok := res.(cachedResult[*lua.FunctionProto]); ok {
				return cachedRes.res, cachedRes.err
			}
		}

		file, found, err := c.loadFile(moduleName)
		if err != nil {
			c.cache.Store(moduleName, cachedResult[*lua.FunctionProto]{err: err})
			return nil, err
		}
		defer file.Close()

		chunk, err := parse.Parse(file, found)
		if err != nil {
			c.cache.Store(moduleName, cachedResult[*lua.FunctionProto]{err: err})
			return nil, err
		}
		compiled, err := lua.Compile(chunk, found)
		if err != nil {
			c.cache.Store(moduleName, cachedResult[*lua.FunctionProto]{err: err})
			return nil, err
		}

		c.cache.Store(moduleName, cachedResult[*lua.FunctionProto]{res: compiled, err: err})
		return compiled, err
	}):
		if res.Err != nil {
			ls.Push(lua.LString(res.Err.Error()))
			return 1
		}

		bytecode, ok := res.Val.(*lua.FunctionProto)
		if !ok {
			ls.Push(lua.LString("Did not receive compiled bytecode"))
			return 1
		}

		ls.Push(ls.NewFunctionFromProto(bytecode))
		return 1

	case <-ls.Context().Done():
		ls.Push(lua.LString("Context canceled"))
		return 1
	}
}

func (c *cachedLoader) loadFile(moduleName string) (fs.File, string, error) {
	moduleNamePath := strings.ReplaceAll(moduleName, ".", "/")
	var found string
	var err error

	for _, path := range c.path {
		filename := path + "/" + moduleNamePath + ".lua"
		_, err := c.workspace.Stat(filename)
		if err == nil {
			found = filename
			break
		}

		filename = path + "/" + moduleNamePath + "/init.lua"
		_, err = c.workspace.Stat(filename)
		if err == nil {
			found = filename
			break
		}
	}

	if found == "" {
		err = fmt.Errorf("Failed to load module %s: could not find matching file", moduleName)
		return nil, "", err
	}

	file, err := c.workspace.Open(found)
	if err != nil {
		err = fmt.Errorf("Failed to load module %s: could not open file %s, got error %s", moduleName, found, err.Error())
		return nil, "", err
	}
	return file, found, nil
}
