// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"testing"
	"time"
)

func TestResultStatus_String(t *testing.T) {
	tests := []struct {
		status ResultStatus
		want   string
	}{
		{Aborted, "Aborted/Skipped"},
		{Success, "Success"},
		{Failure, "Failure"},
		{Error, "Error"},
		{ResultStatus(999), "Unknown(999)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.status.String(); got != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestResultTimer_Uninitialized(t *testing.T) {
	var timer ResultTimer

	if got := timer.Time(); got != time.Duration(0) {
		t.Errorf("Expected 0 duration for uninitialized timer, got %v", got)
	}
}

type mockClock struct {
	currentTime time.Time
}

func (m *mockClock) Now() time.Time {
	return m.currentTime
}

func TestResultTimer_Initialized(t *testing.T) {
	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &mockClock{currentTime: baseTime}

	timer := newTimer(clock)

	clock.currentTime = baseTime.Add(5 * time.Second)

	if got := timer.Time(); got != 5*time.Second {
		t.Errorf("Expected 5s duration, got %v", got)
	}
}
