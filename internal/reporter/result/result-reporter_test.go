// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package result

import (
	"slices"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
)

func stages(statuses ...event.ResultStatus) aggregator.ScenarioResult {
	res := aggregator.ScenarioResult{}
	for _, s := range statuses {
		res.StageResults = append(res.StageResults, aggregator.StageResult{Status: s})
	}
	return res
}

func TestResultReporter(t *testing.T) {
	interrupted := []event.ResultStatus{event.Aborted}
	tests := []struct {
		name    string
		results []aggregator.ScenarioResult
		size    int
		want    []event.ResultStatus
	}{
		{
			name:    "All Success",
			results: []aggregator.ScenarioResult{stages(event.Success, event.Success)},
			want:    []event.ResultStatus{event.Success},
		},
		{
			name:    "Skipped and never reached stages don't fail the run",
			results: []aggregator.ScenarioResult{stages(event.Success, event.Skipped, event.Unset)},
			want:    []event.ResultStatus{event.Success},
		},
		{
			name:    "No scenarios at all",
			results: []aggregator.ScenarioResult{},
			want:    []event.ResultStatus{event.Success},
		},
		{
			name:    "One Failure",
			results: []aggregator.ScenarioResult{stages(event.Success, event.Failure)},
			want:    []event.ResultStatus{event.Failure},
		},
		{
			name:    "Failure as the scenario status",
			results: []aggregator.ScenarioResult{{Status: event.Failure}},
			want:    []event.ResultStatus{event.Failure},
		},
		{
			name:    "One Error",
			results: []aggregator.ScenarioResult{stages(event.Error)},
			want:    []event.ResultStatus{event.Error},
		},
		{
			name:    "Error outranks Failure across scenarios",
			results: []aggregator.ScenarioResult{stages(event.Failure), stages(event.Error), stages(event.Failure)},
			want:    []event.ResultStatus{event.Error},
		},
		{
			name:    "Error outranks Failure within a scenario",
			results: []aggregator.ScenarioResult{stages(event.Error, event.Failure)},
			want:    []event.ResultStatus{event.Error},
		},
		{
			name:    "Request Aborted",
			results: []aggregator.ScenarioResult{stages(event.Aborted)},
			want:    interrupted,
		},
		{
			name:    "Scenario Aborted",
			results: []aggregator.ScenarioResult{{Status: event.Aborted}},
			want:    interrupted,
		},
		{
			name:    "Failure outranks Aborted across scenarios",
			results: []aggregator.ScenarioResult{stages(event.Failure), {Status: event.Aborted}},
			want:    []event.ResultStatus{event.Failure},
		},
		{
			name:    "Failure outranks Aborted within a scenario",
			results: []aggregator.ScenarioResult{stages(event.Failure, event.Aborted)},
			want:    []event.ResultStatus{event.Failure},
		},
		{
			name:    "Error outranks Aborted",
			results: []aggregator.ScenarioResult{stages(event.Error), {Status: event.Aborted}},
			want:    []event.ResultStatus{event.Error},
		},
		{
			name:    "Missing scenarios count as aborted",
			results: []aggregator.ScenarioResult{stages(event.Success)},
			size:    2,
			want:    interrupted,
		},
		{
			name:    "Failure outranks missing scenarios",
			results: []aggregator.ScenarioResult{stages(event.Failure)},
			size:    2,
			want:    []event.ResultStatus{event.Failure},
		},
		{
			name:    "Error outranks missing scenarios",
			results: []aggregator.ScenarioResult{stages(event.Error)},
			size:    2,
			want:    []event.ResultStatus{event.Error},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := NewResultReporter()
			sourceCh := make(chan aggregator.ScenarioResult, len(tt.results))
			timerCh := make(chan time.Duration, 1)

			for _, res := range tt.results {
				sourceCh <- res
			}
			close(sourceCh)
			timerCh <- 1 * time.Millisecond
			close(timerCh)

			size := tt.size
			if size == 0 {
				size = len(tt.results)
			}
			err := rep.Report(sourceCh, timerCh, size)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if got := rep.Status(); !slices.Contains(tt.want, got) {
				t.Errorf("Expected status in %v, got %v", tt.want, got)
			}
		})
	}
}
