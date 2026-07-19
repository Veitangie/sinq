// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package luapi

import (
	"testing"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type MockClock struct {
	CurrentTime time.Time
}

func (m *MockClock) Now() time.Time {
	return m.CurrentTime
}

func TestLuaTime_Now(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	expectedTime := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	mockClock := &MockClock{CurrentTime: expectedTime}

	lc := &LuaContext{
		LState: *L,
		clock:  mockClock,
	}

	L.Push(L.NewFunction(lc.Now))
	if err := L.PCall(0, 1, nil); err != nil {
		t.Fatalf("unexpected lua error: %v", err)
	}

	res := L.Get(-1)
	L.Pop(1)

	if res.Type() != lua.LTNumber {
		t.Fatalf("expected number result, got %v", res.Type())
	}

	if int64(lua.LVAsNumber(res)) != expectedTime.UnixMilli() {
		t.Errorf("expected %d got %f", expectedTime.UnixMilli(), float64(lua.LVAsNumber(res)))
	}
}

func TestLuaTime_FromString(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tests := []struct {
		name      string
		input     string
		format    string
		want      int64
		wantError bool
	}{
		{"Valid ISO8601 with ms", "2026-07-19T12:00:00.123Z", "", 1784462400123, false},
		{"Valid ISO8601 with offset", "2026-07-19T12:00:00.123+05:00", "", 1784444400123, false},
		{"Invalid format", "2026/07/19 12:00:00", "", 0, true},
		{"Missing ms", "2026-07-19T12:00:00Z", "", 0, true},
		{"Custom Format", "2026-07-19", "2006-01-02", 1784419200000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L.Push(L.NewFunction(TimeFromString))
			L.Push(lua.LString(tt.input))

			argsCount := 1
			if tt.format != "" {
				L.Push(lua.LString(tt.format))
				argsCount = 2
			}

			if err := L.PCall(argsCount, 2, nil); err != nil {
				t.Fatalf("unexpected lua error: %v", err)
			}

			errRes := L.Get(-1)
			valRes := L.Get(-2)
			L.Pop(2)

			if tt.wantError {
				if errRes == lua.LNil {
					t.Errorf("expected error string, got nil")
				}
				if valRes != lua.LNil {
					t.Errorf("expected value to be nil on error, got %v", valRes)
				}
			} else {
				if errRes != lua.LNil {
					t.Errorf("expected no error, got %v", errRes)
				}
				if valRes.Type() != lua.LTNumber {
					t.Fatalf("expected number result, got %v", valRes.Type())
				}
				if int64(lua.LVAsNumber(valRes)) != tt.want {
					t.Errorf("expected %d got %f", tt.want, float64(lua.LVAsNumber(valRes)))
				}
			}
		})
	}
}

func TestLuaTime_ToString(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tests := []struct {
		name   string
		input  int64
		format string
		want   string
	}{
		{"Default format", 1784462400123, "", "2026-07-19T12:00:00.123Z"},
		{"Custom format", 1784462400123, "2006/01/02 15:04:05", "2026/07/19 12:00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L.Push(L.NewFunction(TimeToString))
			L.Push(lua.LNumber(tt.input))

			argsCount := 1
			if tt.format != "" {
				L.Push(lua.LString(tt.format))
				argsCount = 2
			}

			if err := L.PCall(argsCount, 1, nil); err != nil {
				t.Fatalf("unexpected lua error: %v", err)
			}

			valRes := L.Get(-1)
			L.Pop(1)

			if valRes.Type() != lua.LTString {
				t.Fatalf("expected string result, got %v", valRes.Type())
			}
			if valRes.String() != tt.want {
				t.Errorf("expected %q got %q", tt.want, valRes.String())
			}
		})
	}
}
