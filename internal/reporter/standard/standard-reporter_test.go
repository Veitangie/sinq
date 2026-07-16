// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/runner"
)

type errorWriter struct{}

func (e errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated write error")
}

func TestStandardReporter_FormatAndColor(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.ReporterConfig
		results    []runner.ScenarioResult
		wantOutput []string
	}{
		{
			name: "Basic Output without Color",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false},
			results: []runner.ScenarioResult{
				{
					Name:   "Test Scenario",
					Status: runner.Success,
					RequestResults: []runner.RequestResult{
						{Name: "Req1", Status: runner.Success},
					},
				},
			},
			wantOutput: []string{" ✓ Scenario: Test Scenario", "   ✓ Req1", " ✓ PASSED in 100ms | Scenarios: 1✓ 0✗ 0○ (1) | 1 requests sent"},
		},
		{
			name: "Output with Color",
			cfg:  config.ReporterConfig{Color: config.Always, Verbose: false},
			results: []runner.ScenarioResult{
				{
					Name:   "Color Scenario",
					Status: runner.Failure,
					RequestResults: []runner.RequestResult{
						{Name: "ReqColor", Status: runner.Failure, ErrorMessage: "boom"},
					},
				},
			},
			wantOutput: []string{Red + "✗" + Reset + " Scenario: Color Scenario", "   " + Red + "✗" + Reset + " ReqColor", "   - Error: boom", Red + "✗" + Reset + " FAILED in 100ms | Scenarios: 0" + Green + "✓" + Reset + " 1" + Red + "✗" + Reset + " 0" + Yellow + "○" + Reset + " (1) | 1 requests sent"},
		},
		{
			name: "Verbose Timings and Assertions",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: true},
			results: []runner.ScenarioResult{
				{
					Name:   "Verbose Scenario",
					Status: runner.Failure,
					RequestResults: []runner.RequestResult{
						{
							Name:             "ReqV",
							Status:           runner.Failure,
							Pre:              1 * time.Millisecond,
							Materialization:  2 * time.Millisecond,
							Parsing:          3 * time.Millisecond,
							Execution:        4 * time.Millisecond,
							Retry:            5 * time.Millisecond,
							Assert:           6 * time.Millisecond,
							Post:             7 * time.Millisecond,
							FailedAssertions: []string{"assert(200) failed", "body check failed"},
						},
					},
				},
			},
			wantOutput: []string{
				"- Pre:         1ms",
				"- Mat:         2ms",
				"- Parse:       3ms",
				"- Exec:        4ms",
				"- Retry:       5ms",
				"- Assert:      6ms",
				"- Post:        7ms",
				"- Failed assertions: assert(200) failed, body check failed",
			},
		},
		{
			name: "Aborted Status Formatting",
			cfg:  config.ReporterConfig{Color: config.Always, Verbose: false},
			results: []runner.ScenarioResult{
				{
					Name:   "Aborted Scenario",
					Status: runner.Aborted,
					RequestResults: []runner.RequestResult{
						{Name: "ReqA", Status: runner.Aborted},
					},
				},
			},
			wantOutput: []string{Yellow + "○" + Reset + " Scenario: Aborted Scenario", "   " + Yellow + "○" + Reset + " ReqA"},
		},
		{
			name: "Skipped Status Formatting",
			cfg:  config.ReporterConfig{Color: config.Always, Verbose: false, Show: config.All},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Skipped,
					RequestResults: []runner.RequestResult{
						{Name: "ReqS", Status: runner.Skipped},
					},
				},
			},
			wantOutput: []string{Gray + "-" + Reset + " Scenario: Skipped Scenario", "   " + Gray + "-" + Reset + " ReqS"},
		},
		{
			name: "Show NoSkip filters Skipped",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.NoSkip},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Skipped,
				},
				{
					Name:   "Success Scenario",
					Status: runner.Success,
				},
			},
			wantOutput: []string{" ✓ Scenario: Success Scenario"},
		},
		{
			name: "Success with skipped scenario prints PASSED",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Skipped,
				},
				{
					Name:   "Success Scenario",
					Status: runner.Success,
				},
			},
			wantOutput: []string{" ✓ PASSED in 100ms | Scenarios: 1✓ 0✗ 1○ (2) | 0 requests sent"},
		},
		{
			name: "Show Failures filters Success and Skipped",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.Failures},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Skipped,
				},
				{
					Name:   "Success Scenario",
					Status: runner.Success,
				},
				{
					Name:   "Failure Scenario",
					Status: runner.Failure,
				},
			},
			wantOutput: []string{" ✗ Scenario: Failure Scenario"},
		},
		{
			name: "Dump On Failure fields",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.All},
			results: []runner.ScenarioResult{
				{
					Name:   "Dump Scenario",
					Status: runner.Failure,
					RequestResults: []runner.RequestResult{
						{
							Name:     "ReqDump",
							Status:   runner.Failure,
							Request:  "GET / HTTP/1.1",
							Response: "HTTP/1.1 500 Internal Server Error",
						},
					},
				},
			},
			wantOutput: []string{"Request:\nGET / HTTP/1.1\n", "Response:\nHTTP/1.1 500 Internal Server Error\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			rep := NewReporter(tt.cfg, buf)

			sourceCh := make(chan runner.ScenarioResult, len(tt.results))
			timerCh := make(chan time.Duration, 1)

			for _, res := range tt.results {
				sourceCh <- res
			}
			close(sourceCh)
			timerCh <- 100 * time.Millisecond
			close(timerCh)

			_ = rep.Report(sourceCh, timerCh, len(tt.results))

			outStr := buf.String()
			for _, want := range tt.wantOutput {
				if !strings.Contains(outStr, want) {
					t.Errorf("Expected output to contain %q, got:\n%s", want, outStr)
				}
			}
		})
	}
}

func TestStandardReporter_WriteErrors(t *testing.T) {
	rep := NewReporter(config.ReporterConfig{}, errorWriter{})
	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- runner.ScenarioResult{
		Name: "Error Test",
		RequestResults: []runner.RequestResult{
			{Name: "ErrReq", Status: runner.Success},
		},
	}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	err := rep.Report(sourceCh, timerCh, 1)
	if err == nil {
		t.Errorf("Expected an error from simulated write failures, got nil")
	}
}
