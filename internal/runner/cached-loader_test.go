// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"testing"
	"testing/fstest"

	lua "github.com/yuin/gopher-lua"
	"golang.org/x/sync/singleflight"
	"veitangie.dev/sinq/internal/luapi"
)

func TestCachedLoader_Load(t *testing.T) {
	fs := fstest.MapFS{
		"test/module.lua":     &fstest.MapFile{Data: []byte("return 'hello'")},
		"test/other/init.lua": &fstest.MapFile{Data: []byte("return 'world'")},
	}

	ws := &mockWorkspace{FS: fs}
	loader := &cachedLoader{
		path:      []string{"test"},
		workspace: ws,
		group:     singleflight.Group{},
	}

	lc := luapi.NewLuaContext(nil, false, nil, nil)
	defer lc.Close()

	lc.SetContext(context.Background())
	callLoad := func(mod string) lua.LValue {
		lc.Push(lc.NewFunction(loader.load))
		lc.Push(lua.LString(mod))
		if err := lc.PCall(1, 1, nil); err != nil {
			t.Fatalf("PCall failed: %v", err)
		}
		ret := lc.Get(-1)
		lc.Pop(1)
		return ret
	}

	ret1 := callLoad("module")
	if ret1.Type() != lua.LTFunction {
		t.Errorf("Expected function, got %v", ret1.Type())
	}

	ret2 := callLoad("other")
	if ret2.Type() != lua.LTFunction {
		t.Errorf("Expected function, got %v", ret2.Type())
	}

	ret3 := callLoad("missing")
	if ret3.Type() != lua.LTString {
		t.Errorf("Expected error string, got %v", ret3.Type())
	}
}
