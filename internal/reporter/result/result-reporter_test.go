// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package result

import (
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
)

func TestResultReporter(t *testing.T) {
	tests := []struct {
		name        string
		results     []aggregator.ScenarioResult
		size        int
		wantSuccess bool
	}{
		{
			name: "All Success",
			results: []aggregator.ScenarioResult{
				{
					RequestResults: []aggregator.RequestResult{
						{Status: event.Success},
						{Status: event.Success},
					},
				},
			},
			wantSuccess: true,
		},
		{
			name: "One Failure",
			results: []aggregator.ScenarioResult{
				{
					RequestResults: []aggregator.RequestResult{
						{Status: event.Success},
						{Status: event.Failure},
					},
				},
			},
			wantSuccess: false,
		},
		{
			name: "One Error",
			results: []aggregator.ScenarioResult{
				{
					RequestResults: []aggregator.RequestResult{
						{Status: event.Error},
					},
				},
			},
			wantSuccess: false,
		},
		{
			name: "Request Aborted counts as failure",
			results: []aggregator.ScenarioResult{
				{
					RequestResults: []aggregator.RequestResult{
						{Status: event.Aborted},
					},
				},
			},
			wantSuccess: false,
		},
		{
			name: "Scenario Aborted counts as failure",
			results: []aggregator.ScenarioResult{
				{
					Status: event.Aborted,
				},
			},
			wantSuccess: false,
		},
		{
			name: "Missing scenarios count as failure",
			results: []aggregator.ScenarioResult{
				{
					RequestResults: []aggregator.RequestResult{
						{Status: event.Success},
					},
				},
			},
			size:        2,
			wantSuccess: false,
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

			if rep.Success() != tt.wantSuccess {
				t.Errorf("Expected success to be %t, got %t", tt.wantSuccess, rep.Success())
			}
		})
	}
}
