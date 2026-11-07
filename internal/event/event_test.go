// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package event_test

import (
	"testing"

	"veitangie.dev/sinq/internal/event"
)

func TestResultStatus_String(t *testing.T) {
	tests := []struct {
		status event.ResultStatus
		want   string
	}{
		{event.Unset, "Skipped"},
		{event.Skipped, "Skipped"},
		{event.Aborted, "Aborted"},
		{event.Success, "Success"},
		{event.Failure, "Failure"},
		{event.Error, "Error"},
		{event.ResultStatus(999), "Unknown(999)"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("ResultStatus(%d): expected %q, got %q", int(tt.status), tt.want, got)
		}
	}
}

func TestSubSystem_String(t *testing.T) {
	tests := []struct {
		source event.SubSystem
		want   string
	}{
		{event.Sinq, "Sinq"},
		{event.Treewalker, "Treewalker"},
		{event.Runner, "Runner"},
		{event.Reporter, "Reporter"},
		{event.EventBus, "EventBus"},
		{event.SubSystem(999), "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.source.String(); got != tt.want {
			t.Errorf("SubSystem(%d): expected %q, got %q", int(tt.source), tt.want, got)
		}
	}
}
