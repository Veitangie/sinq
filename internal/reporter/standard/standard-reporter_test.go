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
	"github.com/Veitangie/sinq/internal/ui"
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
			wantOutput: []string{" ✓ Scenario: Test Scenario", " ✓ PASSED in 100ms | Scenarios: 1✓ | 1 requests sent"},
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
			wantOutput: []string{ui.Red + "✗" + ui.Reset + " Scenario: Color Scenario", "   " + ui.Red + "✗" + ui.Reset + " ReqColor", "     " + ui.Red + "✗" + ui.Reset + " Error: boom", ui.Red + "✗" + ui.Reset + " FAILED in 100ms | Scenarios: 1" + ui.Red + "✗" + ui.Reset + " | 1 requests sent"},
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
				"     ┃ Pre:         1ms",
				"     ┃ Mat:         2ms",
				"     ┃ Parse:       3ms",
				"     ┃ Exec:        4ms",
				"     ┃ Retry:       5ms",
				"     ┃ Assert:      6ms",
				"     ┃ Post:        7ms",
				"     ✗ Failed assertions: assert(200) failed, body check failed",
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
			wantOutput: []string{ui.Yellow + "○" + ui.Reset + " Scenario: Aborted Scenario", "   " + ui.Yellow + "○" + ui.Reset + " ReqA"},
		},
		{
			name: "Skipped Status Formatting",
			cfg:  config.ReporterConfig{Color: config.Always, Verbose: false, Show: config.All},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Unset,
					RequestResults: []runner.RequestResult{
						{Name: "ReqS", Status: runner.Unset},
					},
				},
			},
			wantOutput: []string{ui.Gray + "-" + ui.Reset + " Scenario: Skipped Scenario", " " + ui.Gray + "-" + ui.Reset + " SKIPPED in 100ms | Scenarios: 1" + ui.Gray + "-" + ui.Reset + " | 0 requests sent"},
		},
		{
			name: "Show NoSkip filters Skipped",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.NoSkip},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Unset,
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
					Status: runner.Unset,
				},
				{
					Name:   "Success Scenario",
					Status: runner.Success,
				},
			},
			wantOutput: []string{" ✓ PASSED in 100ms | Scenarios: 1✓ 1- | 0 requests sent"},
		},
		{
			name: "Show Failures filters Success and Skipped",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.Failed},
			results: []runner.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: runner.Unset,
				},
				{
					Name:   "Success Scenario",
					Status: runner.Success,
					RequestResults: []runner.RequestResult{
						{Name: "Req1", Status: runner.Success},
						{Name: "Req2", Status: runner.Unset},
					},
				},
				{
					Name:   "Failure Scenario",
					Status: runner.Failure,
					RequestResults: []runner.RequestResult{
						{Name: "Req3", Status: runner.Failure},
					},
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
							Request:  "GET / HTTP/1.1\n",
							Response: "HTTP/1.1 500 Internal Server Error\n",
						},
					},
				},
			},
			wantOutput: []string{"     Request:\n     ┃ GET / HTTP/1.1", "     Response:\n     ┃ HTTP/1.1 500 Internal Server Error"},
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

func TestStandardReporter_OutputPrint(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Verbose: false}, buf)

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	outputBuf := bytes.NewBufferString("hello\nworld\n")
	sourceCh <- runner.ScenarioResult{
		Name:   "Print Scenario",
		Status: runner.Failure,
		Output: outputBuf,
		RequestResults: []runner.RequestResult{
			{Name: "Req", Status: runner.Failure},
		},
	}
	close(sourceCh)
	timerCh <- 10 * time.Millisecond
	close(timerCh)

	_ = rep.Report(sourceCh, timerCh, 1)

	outStr := buf.String()
	want := "   Output:\n   ┃ hello\n   ┃ world\n"
	if !strings.Contains(outStr, want) {
		t.Errorf("Wanted: %s, got: %s", want, outStr)
	}
}
