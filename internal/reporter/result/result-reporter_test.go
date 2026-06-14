// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package result

import (
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/runner"
)

func TestResultReporter(t *testing.T) {
	tests := []struct {
		name        string
		results     []runner.ScenarioResult
		wantSuccess bool
	}{
		{
			name: "All Success",
			results: []runner.ScenarioResult{
				{
					RequestResults: []runner.RequestResult{
						{Status: runner.Success},
						{Status: runner.Success},
					},
				},
			},
			wantSuccess: true,
		},
		{
			name: "One Failure",
			results: []runner.ScenarioResult{
				{
					RequestResults: []runner.RequestResult{
						{Status: runner.Success},
						{Status: runner.Failure},
					},
				},
			},
			wantSuccess: false,
		},
		{
			name: "One Error",
			results: []runner.ScenarioResult{
				{
					RequestResults: []runner.RequestResult{
						{Status: runner.Error},
					},
				},
			},
			wantSuccess: false,
		},
		{
			name: "Aborted counts as success",
			results: []runner.ScenarioResult{
				{
					RequestResults: []runner.RequestResult{
						{Status: runner.Aborted},
					},
				},
			},
			wantSuccess: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := NewResultReporter()
			sourceCh := make(chan runner.ScenarioResult, len(tt.results))
			timerCh := make(chan time.Duration, 1)

			for _, res := range tt.results {
				sourceCh <- res
			}
			close(sourceCh)
			timerCh <- 1 * time.Millisecond
			close(timerCh)

			err := rep.Report(sourceCh, timerCh, len(tt.results))
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if rep.Success() != tt.wantSuccess {
				t.Errorf("Expected success to be %t, got %t", tt.wantSuccess, rep.Success())
			}
		})
	}
}
