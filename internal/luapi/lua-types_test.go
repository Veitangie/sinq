// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"os"
	"regexp"
	"slices"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/timer"
)

var stubName = regexp.MustCompile(`(?m)^(?:function )?((?:sinq|req)(?:\.\w+)+)`)

func collectNames(prefix string, table *lua.LTable, names map[string]bool) {
	table.ForEach(func(key, value lua.LValue) {
		if key.Type() != lua.LTString {
			return
		}
		name := prefix + "." + key.String()
		names[name] = true
		if nested, ok := value.(*lua.LTable); ok {
			collectNames(name, nested, names)
		}
	})
}

func TestLuaTypes_StubMatchesTheAPI(t *testing.T) {
	stub, err := os.ReadFile("../../lua-types/sinq.lua")
	if err != nil {
		t.Fatalf("Failed to read the stub: %v", err)
	}

	noop := func(*lua.LState) int { return 0 }
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(noop, noop, noop, map[string]any{}, map[string]any{})
	lc.SetupStageEnvironment(0, true)
	lc.SetupPreScript(noop, noop, noop, noop, noop, noop)
	lc.SetupRetryScript()
	lc.SetupAssertScript(noop)

	real := map[string]bool{}
	collectNames("sinq", lc.sinqTable, real)
	collectNames("req", lc.RequestTable, real)

	documented := map[string]bool{}
	for _, match := range stubName.FindAllSubmatch(stub, -1) {
		documented[string(match[1])] = true
	}

	for _, name := range sortedKeys(real) {
		if !documented[name] {
			t.Errorf("%s exists in the API but not in lua-types/sinq.lua", name)
		}
	}
	for _, name := range sortedKeys(documented) {
		if !real[name] {
			t.Errorf("%s is in lua-types/sinq.lua but not in the API", name)
		}
	}
}

func sortedKeys(names map[string]bool) []string {
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}
