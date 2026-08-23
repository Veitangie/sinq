// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"fmt"
	"strings"
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
	frame := GetSimpleFrame(false, clock, "Running")

	for i, state := range spinq.DotsStates {
		if i > 0 {
			clock.now = clock.now.Add(100 * time.Millisecond)
		}

		got, err := frame()
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}

		expected := fmt.Sprintf(" %s Running (%.1fs)\r", state, float64(i)*0.1)
		if string(got) != expected {
			t.Errorf("call %d: got %q, want %q", i, got, expected)
		}
	}
}

func TestGetFrame_NoColor_CyclesStates(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(false, clock, "Running")

	for i := 0; i < len(spinq.DotsStates); i++ {
		if _, err := frame(); err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
	}

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf(" %s Running (0.0s)\r", spinq.DotsStates[0])
	if string(got) != expected {
		t.Errorf("got %q, want %q (state should have wrapped around)", got, expected)
	}
}

func TestGetFrame_Color(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(true, clock, "Running")

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s%s%s Running (0.0s)\r", ColorStates[0], spinq.DotsStates[0], spinq.ResetColor)
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestGetFrame_Color_AdvancesOncePerFullSpinnerCycle(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(true, clock, "Running")

	for i := 0; i < len(spinq.DotsStates); i++ {
		got, err := frame()
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		expected := fmt.Sprintf(" %s%s%s Running (0.0s)\r", ColorStates[0], spinq.DotsStates[i], spinq.ResetColor)
		if string(got) != expected {
			t.Errorf("call %d: got %q, want %q", i, got, expected)
		}
	}

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := fmt.Sprintf(" %s%s%s Running (0.0s)\r", ColorStates[1], spinq.DotsStates[0], spinq.ResetColor)
	if string(got) != expected {
		t.Errorf("got %q, want %q (color should have advanced)", got, expected)
	}
}

func TestGetFrame_UsesGivenClock(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(false, clock, "Running")

	if _, err := frame(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.now = clock.now.Add(2500 * time.Millisecond)
	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s Running (2.5s)\r", spinq.DotsStates[1])
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestGetStartingFrame_NoColor(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(false, clock, "Starting")

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s Starting (0.0s)\r", spinq.DotsStates[0])
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestGetStartingFrame_Color(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(true, clock, "Starting")

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s%s%s Starting (0.0s)\r", ColorStates[0], spinq.DotsStates[0], spinq.ResetColor)
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestGetStartingFrame_UsesGivenClock(t *testing.T) {
	clock := &mockClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	frame := GetSimpleFrame(false, clock, "Starting")

	if _, err := frame(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	clock.now = clock.now.Add(2500 * time.Millisecond)
	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := fmt.Sprintf(" %s Starting (2.5s)\r", spinq.DotsStates[1])
	if string(got) != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestGetBarFrame_NoColor_NoAnsiCodes(t *testing.T) {
	progress := func() (int, int) { return 3, 42 }
	getWidth := func() int { return 60 }
	frame := GetBarFrame(false, 42, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotStr := string(got)

	if strings.Contains(gotStr, "\033[") {
		t.Errorf("expected no ANSI escape codes with color=false, got %q", gotStr)
	}

	wantPrefix := fmt.Sprintf(" %s Running | ", spinq.DotsStates[0])
	if !strings.HasPrefix(gotStr, wantPrefix) {
		t.Errorf("got %q, want prefix %q", gotStr, wantPrefix)
	}

	wantSuffix := " |  3/42\r"
	if !strings.HasSuffix(gotStr, wantSuffix) {
		t.Errorf("got %q, want suffix %q", gotStr, wantSuffix)
	}
}

func TestGetBarFrame_Color_WrapsDotsAndBarInAnsiCodes(t *testing.T) {
	progress := func() (int, int) { return 3, 42 }
	getWidth := func() int { return 60 }
	frame := GetBarFrame(true, 42, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotStr := string(got)

	if !strings.Contains(gotStr, spinq.Green) {
		t.Errorf("expected bar to be wrapped in green, got %q", gotStr)
	}
	if !strings.Contains(gotStr, spinq.ResetColor) {
		t.Errorf("expected color reset codes present, got %q", gotStr)
	}

	wantPrefix := fmt.Sprintf(" %s%s%s Running | %s", ColorStates[0], spinq.DotsStates[0], spinq.ResetColor, spinq.Green)
	if !strings.HasPrefix(gotStr, wantPrefix) {
		t.Errorf("got %q, want prefix %q", gotStr, wantPrefix)
	}

	wantSuffix := spinq.ResetColor + " |  3/42\r"
	if !strings.HasSuffix(gotStr, wantSuffix) {
		t.Errorf("got %q, want suffix %q", gotStr, wantSuffix)
	}
}

func TestGetBarFrame_EndsInCarriageReturn(t *testing.T) {
	progress := func() (int, int) { return 1, 5 }
	getWidth := func() int { return 60 }
	frame := GetBarFrame(false, 5, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(got) == 0 || got[len(got)-1] != '\r' {
		t.Errorf("expected frame to end in a carriage return, got %q", got)
	}
	if strings.Count(string(got), "\r") != 1 {
		t.Errorf("expected exactly one carriage return, got %q", got)
	}
}

func TestGetBarFrame_ZeroTotal_DegradesGracefully(t *testing.T) {
	progress := func() (int, int) { return 0, 0 }
	getWidth := func() int { return 60 }
	frame := GetBarFrame(false, 0, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(string(got), "/") {
		t.Errorf("expected no fraction rendered for a zero total, got %q", got)
	}
}

func TestGetBarFrame_NarrowWidth_OmitsBarKeepsFraction(t *testing.T) {
	progress := func() (int, int) { return 3, 42 }
	getWidth := func() int { return 0 }
	frame := GetBarFrame(false, 42, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotStr := string(got)

	if strings.Contains(gotStr, "Running") {
		t.Errorf("expected the bar label to be dropped when there's no room, got %q", gotStr)
	}

	want := fmt.Sprintf(" %s |  3/42\r", spinq.DotsStates[0])
	if gotStr != want {
		t.Errorf("got %q, want %q", gotStr, want)
	}
}

func TestGetBarFrame_WidthAtBoundary_NoBar(t *testing.T) {
	progress := func() (int, int) { return 3, 5 }
	getWidth := func() int { return 21 }
	frame := GetBarFrame(false, 5, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotStr := string(got)

	if strings.Contains(gotStr, "Running") {
		t.Errorf("expected the bar to be dropped at width 21 (no room), got %q", gotStr)
	}
}

func TestGetBarFrame_WidthJustOverBoundary_ShowsBar(t *testing.T) {
	progress := func() (int, int) { return 3, 5 }
	getWidth := func() int { return 22 }
	frame := GetBarFrame(false, 5, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotStr := string(got)

	if !strings.Contains(gotStr, "Running") {
		t.Errorf("expected the bar to appear at width 22 (one cell of room), got %q", gotStr)
	}
}

func TestGetBarFrame_NoColor_EmptyCellsAreSpaces(t *testing.T) {
	progress := func() (int, int) { return 0, 10 }
	getWidth := func() int { return 33 }
	frame := GetBarFrame(false, 10, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantBar := " Running | " + "━" + strings.Repeat(" ", 10)
	want := " " + spinq.DotsStates[0] + wantBar + " | " + " 0/10" + "\r"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGetBarFrame_Color_EmptyCellsMatchFullGlyphInBlack(t *testing.T) {
	progress := func() (int, int) { return 0, 10 }
	getWidth := func() int { return 33 }
	frame := GetBarFrame(true, 10, progress, getWidth)

	got, err := frame()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantBar := " Running | " + spinq.Green + "━" + black + strings.Repeat("━", 10) + spinq.ResetColor
	want := " " + ColorStates[0] + spinq.DotsStates[0] + spinq.ResetColor + wantBar + " | " + " 0/10" + "\r"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGetBarFrame_WidthCappedAt50(t *testing.T) {
	progress := func() (int, int) { return 3, 5 }

	frameWide := GetBarFrame(false, 5, progress, func() int { return 200 })
	gotWide, err := frameWide()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	frameWider := GetBarFrame(false, 5, progress, func() int { return 2000 })
	gotWider, err := frameWider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(gotWide) != string(gotWider) {
		t.Errorf("expected the bar to stop growing once capped, got %q at width 200 and %q at width 2000", gotWide, gotWider)
	}
}

var _ timer.Clock = &mockClock{}
