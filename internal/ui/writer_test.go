// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"fmt"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/spinq"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func TestGetFrame_NoColor(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetFrame(false, clock)

	for i, state := range spinq.DotsStates {
		if i > 0 {
			clock.now = clock.now.Add(100 * time.Millisecond)
		}

		got, err := frame()
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}

		expected := fmt.Sprintf(" %s Running (%.1fs)", state, float64(i)*0.1)
		if string(got) != expected {
			t.Errorf("call %d: got %q, want %q", i, got, expected)
		}
	}
}

func TestGetFrame_NoColor_CyclesStates(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetFrame(false, clock)

	for i := 0; i < len(spinq.DotsStates); i++ {
		if _, err := frame(); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf(" %s Running (0.0s)", spinq.DotsStates[0])
	if string(got) != expected {
		t.Errorf("got %q, want %q (state should have wrapped around)", got, expected)
	}
}

func TestGetFrame_Color(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetFrame(true, clock)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s%s%s Running (0.0s)", ColorStates[0], spinq.DotsStates[0], spinq.ResetColor)
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestGetFrame_Color_AdvancesOncePerFullSpinnerCycle(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetFrame(true, clock)

	for i := 0; i < len(spinq.DotsStates); i++ {
		got, err := frame()
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		expected := fmt.Sprintf(" %s%s%s Running (0.0s)", ColorStates[0], spinq.DotsStates[i], spinq.ResetColor)
		if string(got) != expected {
			t.Errorf("call %d: got %q, want %q", i, got, expected)
		}
	}

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf(" %s%s%s Running (0.0s)", ColorStates[1], spinq.DotsStates[0], spinq.ResetColor)
	if string(got) != expected {
		t.Errorf("got %q, want %q (color should have advanced)", got, expected)
	}
}

func TestGetFrame_UsesGivenClock(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetFrame(false, clock)

	if _, err := frame(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.now = clock.now.Add(2500 * time.Millisecond)
	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s Running (2.5s)", spinq.DotsStates[1])
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

var _ timer.Clock = &mockClock{}
