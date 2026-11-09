// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
	"veitangie.dev/sinq/internal/timer"
)

func runSinqScript(t *testing.T, script string) {
	t.Helper()
	noop := func(*lua.LState) int { return 0 }
	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	defer lc.Close()
	lc.SetupScenarioEnvironment(noop, noop, noop, map[string]any{}, map[string]any{})

	if err := lc.DoString(script); err != nil {
		t.Error(err)
	}
}

func TestLuaRegex_Compile(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"valid pattern", `
			local re, err = sinq.regex.compile("a+")
			assert(type(re) == "table", "expected a regex table, got " .. type(re))
			assert(err == nil, "expected no error, got " .. tostring(err))
		`},
		{"invalid pattern", `
			local re, err = sinq.regex.compile("(")
			assert(re == nil, "expected no regex")
			assert(type(err) == "string" and err:find("^sinq.regex.compile: "), "expected a prefixed error, got " .. tostring(err))
		`},
		{"non-string pattern", `
			local re, err = sinq.regex.compile(42)
			assert(re == nil, "expected no regex")
			assert(type(err) == "string" and err:find("number"), "expected the error to name the type, got " .. tostring(err))
		`},
		{"missing pattern", `
			local re, err = sinq.regex.compile()
			assert(re == nil and type(err) == "string", "expected an error for a missing pattern")
		`},
		{"group named full is reserved", `
			local re, err = sinq.regex.compile("(?P<full>a)")
			assert(re == nil, "expected no regex")
			assert(type(err) == "string" and err:find("full"), "expected the error to name the group, got " .. tostring(err))
		`},
		{"full is only reserved as a whole name", `
			local re, err = sinq.regex.compile("(?P<fullName>a)")
			assert(re ~= nil, "expected fullName to be allowed, got " .. tostring(err))
		`},
		{"duplicate group names are rejected", `
			local re, err = sinq.regex.compile("(?P<a>x)(?P<a>y)")
			assert(re == nil, "expected no regex")
			assert(type(err) == "string" and err:find("duplicate"), "expected a duplicate-name error, got " .. tostring(err))
		`},
		{"several unnamed groups are fine", `
			local re, err = sinq.regex.compile("(a)(b)(?P<n>c)(d)")
			assert(re ~= nil, "expected unnamed groups not to count as duplicates, got " .. tostring(err))
		`},
		{"lookahead is rejected", `
			local re, err = sinq.regex.compile("a(?=b)")
			assert(re == nil and type(err) == "string", "expected RE2 to reject a lookahead")
		`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runSinqScript(t, tt.script) })
	}
}

func TestLuaRegex_Match(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"full match and groups", `
			local m = sinq.regex.compile("(\\d+)-(\\d+)").match("id 12-34 end")
			assert(m.full == "12-34", "full: " .. tostring(m.full))
			assert(m[1] == "12" and m[2] == "34", "groups: " .. tostring(m[1]) .. ", " .. tostring(m[2]))
		`},
		{"first match only", `
			local m = sinq.regex.compile("\\d+").match("1 22 333")
			assert(m.full == "1", "expected the first match, got " .. tostring(m.full))
		`},
		{"no match is nil", `
			local m, err = sinq.regex.compile("x").match("abc")
			assert(m == nil, "expected nil for no match")
			assert(err == nil, "expected no error, got " .. tostring(err))
		`},
		{"no groups means an empty array part", `
			local m = sinq.regex.compile("b").match("abc")
			assert(m.full == "b", "full: " .. tostring(m.full))
			assert(#m == 0, "expected no groups, got " .. #m)
		`},
		{"there is no [0]", `
			local m = sinq.regex.compile("(b)").match("abc")
			assert(m[0] == nil, "expected the whole match only under full")
		`},
		{"named group by index and by name", `
			local m = sinq.regex.compile("(?P<year>\\d{4})-(\\d{2})").match("2026-11")
			assert(m.year == "2026" and m[1] == "2026", "year: " .. tostring(m.year) .. ", " .. tostring(m[1]))
			assert(m[2] == "11", "month: " .. tostring(m[2]))
			assert(#m == 2, "expected 2 groups, got " .. #m)
		`},
		{"(?<name>) syntax", `
			local m = sinq.regex.compile("(?<word>\\w+)").match("hi")
			assert(m.word == "hi", "word: " .. tostring(m.word))
		`},
		{"non-capturing groups aren't numbered", `
			local m = sinq.regex.compile("(?:a)(b)").match("ab")
			assert(m[1] == "b", "first group: " .. tostring(m[1]))
			assert(m[2] == nil and #m == 1, "expected only one group")
		`},
		{"a numeric name stays a string key", `
			local m = sinq.regex.compile("(?P<1>a)(b)").match("ab")
			assert(m["1"] == "a", "named: " .. tostring(m["1"]))
			assert(m[1] == "a" and m[2] == "b", "indexed: " .. tostring(m[1]) .. ", " .. tostring(m[2]))
		`},
		{"a group that didn't take part is false", `
			local re = sinq.regex.compile("^/items(\\?(?P<query>.*))?$")
			local absent, empty = re.match("/items"), re.match("/items?")
			assert(absent[1] == false and absent[2] == false, "absent by index: " .. tostring(absent[2]))
			assert(absent.query == false, "absent by name: " .. tostring(absent.query))
			assert(empty[2] == "" and empty.query == "", "expected an empty query to stay an empty string")
		`},
		{"false keeps the array part whole", `
			local m = sinq.regex.compile("(a)(b)?(c)").match("ac")
			assert(#m == 3, "expected 3 groups, got " .. #m)
			assert(m[1] == "a" and m[2] == false and m[3] == "c", "groups: " .. tostring(m[1]) .. ", " .. tostring(m[2]) .. ", " .. tostring(m[3]))
			local n = 0
			for _ in ipairs(m) do n = n + 1 end
			assert(n == 3, "expected ipairs to visit 3 groups, got " .. n)
		`},
		{"multibyte text", `
			local m = sinq.regex.compile("(é+)").match("caféé!")
			assert(m[1] == "éé", "group: " .. tostring(m[1]))
		`},
		{"empty match", `
			local m = sinq.regex.compile("a*").match("bbb")
			assert(m ~= nil and m.full == "", "expected an empty match, got " .. tostring(m and m.full))
		`},
		{"results are independent", `
			local re = sinq.regex.compile("(\\w)")
			local a, b = re.match("x"), re.match("y")
			a[1] = "changed"
			assert(b[1] == "y", "expected the second result untouched, got " .. tostring(b[1]))
		`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runSinqScript(t, tt.script) })
	}
}

func TestLuaRegex_MatchAll(t *testing.T) {
	tests := []struct {
		name   string
		script string
	}{
		{"every match in order", `
			local all = sinq.regex.compile("(?P<k>\\w)=(\\d)").matchAll("a=1, b=2, c=3")
			assert(#all == 3, "expected 3 matches, got " .. #all)
			for i, want in ipairs({ "a", "b", "c" }) do
				assert(all[i].k == want and all[i][1] == want, "match " .. i .. ": " .. tostring(all[i].k))
				assert(all[i][2] == tostring(i), "value " .. i .. ": " .. tostring(all[i][2]))
				assert(all[i].full == want .. "=" .. i, "full " .. i .. ": " .. tostring(all[i].full))
			end
		`},
		{"matches don't overlap", `
			local all = sinq.regex.compile("aa").matchAll("aaaa")
			assert(#all == 2, "expected 2 non-overlapping matches, got " .. #all)
		`},
		{"each match tracks its own optional groups", `
			local all = sinq.regex.compile("(\\d)(x)?").matchAll("1x2")
			assert(all[1][2] == "x" and all[2][2] == false, "groups: " .. tostring(all[1][2]) .. ", " .. tostring(all[2][2]))
		`},
		{"no match is an empty table", `
			local all = sinq.regex.compile("x").matchAll("abc")
			assert(type(all) == "table" and #all == 0, "expected an empty table, got " .. tostring(all))
			for _ in ipairs(all) do error("expected nothing to iterate") end
		`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { runSinqScript(t, tt.script) })
	}
}

func TestLuaRegex_ColonCalls(t *testing.T) {
	runSinqScript(t, `
		local re = sinq.regex.compile("(\\d)")
		assert(re:match("a1").full == re.match("a1").full, "match: colon and dot calls differ")
		assert(#re:matchAll("1 2") == #re.matchAll("1 2"), "matchAll: colon and dot calls differ")
	`)
}

func TestLuaRegex_NonStringSubjectRaises(t *testing.T) {
	for _, call := range []string{
		"re.match(nil)", "re:match(nil)", "re.match({})",
		"re.matchAll(nil)", "re:matchAll(nil)", "re.matchAll({})",
	} {
		t.Run(call, func(t *testing.T) {
			runSinqScript(t, `
				local re = sinq.regex.compile("a")
				local ok, err = pcall(function() return `+call+` end)
				assert(not ok, "expected an error")
				assert(tostring(err):find("string expected"), "expected a type error, got " .. tostring(err))
			`)
		})
	}
}

func TestLuaRegex_MethodsKeepTheirOwnRegex(t *testing.T) {
	runSinqScript(t, `
		local digits = sinq.regex.compile("\\d+")
		local letters = sinq.regex.compile("[a-z]+")
		local match = digits.match
		assert(match("ab12").full == "12", "a method taken off its table should keep its regex")
		assert(letters:match("ab12").full == "ab", "each regex should use its own pattern")
	`)
}
