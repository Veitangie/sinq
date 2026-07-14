// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package timer

import (
	"testing"
	"time"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func TestNewTimer(t *testing.T) {
	timer := NewTimer(nil)
	if timer.clock == nil {
		t.Error("NewTimer(nil) did not set a default clock")
	}
	_, isDefault := timer.clock.(DefaultClock)
	if !isDefault {
		t.Error("NewTimer(nil) did not set DefaultClock")
	}

	mock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	timer = NewTimer(mock)
	if !timer.initialized {
		t.Error("NewTimer did not initialize timer")
	}
	if timer.at != mock.now {
		t.Errorf("Expected timer.at to be %v, got %v", mock.now, timer.at)
	}
}

func TestTimer_Start(t *testing.T) {
	mock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	var timer Timer
	timer.clock = mock

	if timer.initialized {
		t.Error("Expected timer to be uninitialized initially")
	}

	mock.now = mock.now.Add(5 * time.Second)
	timer.Start()

	if !timer.initialized {
		t.Error("Start() did not initialize timer")
	}
	if timer.at != mock.now {
		t.Errorf("Expected Start() to set at to %v, got %v", mock.now, timer.at)
	}
}

func TestTimer_Time(t *testing.T) {
	var uninitTimer Timer
	if uninitTimer.Time() != 0 {
		t.Errorf("Expected Time() of uninitialized timer to be 0, got %v", uninitTimer.Time())
	}

	mock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	timer := NewTimer(mock)

	mock.now = mock.now.Add(10 * time.Second)
	if timer.Time() != 10*time.Second {
		t.Errorf("Expected Time() to return 10s, got %v", timer.Time())
	}
}

func TestTimer_StartedAt(t *testing.T) {
	mock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	timer := NewTimer(mock)

	if timer.StartedAt() != mock.now {
		t.Errorf("Expected StartedAt() to return %v, got %v", mock.now, timer.StartedAt())
	}
}
