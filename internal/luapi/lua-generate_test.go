// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

func TestLuaFaker(t *testing.T) {
	lc := NewLuaContext(nil, false, "")
	defer lc.Close()

	lc.SetupScenarioEnvironment(
		func(ls *lua.LState) int { return 0 },
		func(ls *lua.LState) int { return 0 },
		func(ls *lua.LState) int { return 0 },
		nil,
		nil,
	)

	script := `
		sinq.fake.setSeed(1337)
		local u1 = sinq.fake.uuid()
		local e1 = sinq.fake.email()
		local t1 = sinq.fake.trace()
		
		sinq.fake.setSeed(1337)
		local u2 = sinq.fake.uuid()
		local e2 = sinq.fake.email()
		local t2 = sinq.fake.trace()
		
		assert(u1 == u2, "Seeding is not deterministic for uuid")
		assert(e1 == e2, "Seeding is not deterministic for email")
		assert(t1 == t2, "Seeding is not deterministic for trace")

		assert(type(sinq.fake.uuid()) == "string")
		assert(type(sinq.fake.email()) == "string")
		assert(type(sinq.fake.phone()) == "string")
		assert(type(sinq.fake.name()) == "string")
		assert(type(sinq.fake.firstName()) == "string")
		assert(type(sinq.fake.lastName()) == "string")
		assert(type(sinq.fake.username()) == "string")
		assert(type(sinq.fake.password()) == "string")
		assert(type(sinq.fake.word()) == "string")
		assert(type(sinq.fake.address()) == "string")
		assert(type(sinq.fake.company()) == "string")
		assert(type(sinq.fake.ipv4()) == "string")
		assert(type(sinq.fake.ipv6()) == "string")
		assert(type(sinq.fake.url()) == "string")
		assert(type(sinq.fake.userAgent()) == "string")
		
		local trace = sinq.fake.trace()
		assert(type(trace) == "string")
		assert(string.match(trace, "^00%-[a-f0-9]+%-[a-f0-9]+%-01$"), "Trace format invalid")
		
		local ts = sinq.fake.timestamp(1000)
		assert(type(ts) == "number")
		
		local intVal = sinq.fake.int(1, 10)
		assert(type(intVal) == "number" and intVal >= 1 and intVal <= 10)
		
		local floatVal = sinq.fake.float(2, 1, 10)
		assert(type(floatVal) == "number" and floatVal >= 1 and floatVal <= 10)
		
		local sh = sinq.fake.shakespeare()
		assert(type(sh) == "boolean")
		
		local picked = sinq.fake.oneOf({"a", "b", "c"})
		assert(picked == "a" or picked == "b" or picked == "c")
	`

	fn, err := lc.LoadString(script)
	if err != nil {
		t.Fatalf("Failed to compile lua test script: %v", err)
	}

	err = lc.RunSandboxed(fn.Proto, 2*time.Second)
	if err != nil {
		t.Fatalf("Failed to execute lua test script: %v", err)
	}
}
