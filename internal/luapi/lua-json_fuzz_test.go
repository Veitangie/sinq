// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"veitangie.dev/sinq/internal/timer"
)

func FuzzJSONRoundTrip(f *testing.F) {
	for _, seed := range []string{
		`{}`, `[]`, `[[],{}]`, `null`, `true`, `-0`, `0.1`, `1e308`, `"\ud800"`, `"\u0000"`,
		`{"1":"a","2":"b"}`, `{"0":"x"}`, `{"a":1,"a":2}`, `[null]`, `[1,null,3]`, `{"a":null}`,
		`{"a":{"1":true},"b":[{"c":[1,2,{"d":"e"}]}]}`, `123456789012345678901`, `"tab\tquote\"slash\\"`,
	} {
		f.Add(seed)
	}

	lc := NewLuaContext(timer.DefaultClock{}, false, nil, nil)
	f.Cleanup(lc.Close)

	f.Fuzz(func(t *testing.T, input string) {
		if !json.Valid([]byte(input)) {
			return
		}
		parsed, err := lc.parser.Parse(input)
		if err != nil {
			return
		}

		out, err := lc.serializer.SerializeLValue(parsed)
		if err != nil {
			t.Fatalf("Parsed %q but couldn't serialize it back: %v", input, err)
		}
		if !json.Valid([]byte(out)) {
			t.Fatalf("Serialized %q into invalid JSON %q", input, out)
		}

		var want, got any
		if err := json.Unmarshal([]byte(input), &want); err != nil {
			t.Skipf("Go can't decode %q: %v", input, err)
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("Go can't decode the serialized %q: %v", out, err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("Round trip changed the meaning of %q:\nwant %#v\ngot  %#v (serialized %q)", input, want, got, out)
		}

		reparsed, err := lc.parser.Parse(out)
		if err != nil {
			t.Fatalf("Couldn't parse our own output %q: %v", out, err)
		}
		again, err := lc.serializer.SerializeLValue(reparsed)
		if err != nil {
			t.Fatalf("Couldn't serialize %q a second time: %v", out, err)
		}
		if again != out {
			t.Fatalf("Second round trip isn't stable:\nfirst  %q\nsecond %q", out, again)
		}
	})
}
