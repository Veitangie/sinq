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

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/spinq"
)

type errorWriter struct{}

func (e errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated write error")
}

func TestStandardReporter_FormatAndColor(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.ReporterConfig
		results    []aggregator.ScenarioResult
		wantOutput []string
	}{
		{
			name: "Basic Output without Color",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Test Scenario",
					Status: event.Success,
					RequestResults: []aggregator.RequestResult{
						{Name: "Req1", Status: event.Success, Attempts: 1},
					},
				},
			},
			wantOutput: []string{" ✓ Scenario: Test Scenario", " ✓ PASSED in 100ms | Scenarios: 1✓ | 1 request sent"},
		},
		{
			name: "Output with Color",
			cfg:  config.ReporterConfig{Color: config.Always, Verbose: false},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Color Scenario",
					Status: event.Failure,
					RequestResults: []aggregator.RequestResult{
						{Name: "ReqColor", Status: event.Failure, ErrorMessage: "boom", Attempts: 1},
					},
				},
			},
			wantOutput: []string{spinq.Red + "✗" + spinq.ResetStyle + " Scenario: Color Scenario", "   " + spinq.Red + "✗" + spinq.ResetStyle + " ReqColor", "     " + spinq.Red + "✗" + spinq.ResetStyle + " Error: boom", spinq.Red + "✗" + spinq.ResetStyle + " FAILED in 100ms | Scenarios: 1" + spinq.Red + "✗" + spinq.ResetStyle + " | 1 request sent"},
		},
		{
			name: "Verbose Timings and Assertions",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: true},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Verbose Scenario",
					Status: event.Failure,
					RequestResults: []aggregator.RequestResult{
						{
							Name:             "ReqV",
							Status:           event.Failure,
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
			results: []aggregator.ScenarioResult{
				{
					Name:   "Aborted Scenario",
					Status: event.Aborted,
					RequestResults: []aggregator.RequestResult{
						{Name: "ReqA", Status: event.Aborted},
					},
				},
			},
			wantOutput: []string{spinq.Yellow + "○" + spinq.ResetStyle + " Scenario: Aborted Scenario", "   " + spinq.Yellow + "○" + spinq.ResetStyle + " ReqA"},
		},
		{
			name: "Skipped Status Formatting",
			cfg:  config.ReporterConfig{Color: config.Always, Verbose: false, Show: config.All},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: event.Unset,
					RequestResults: []aggregator.RequestResult{
						{Name: "ReqS", Status: event.Unset},
					},
				},
			},
			wantOutput: []string{spinq.Gray + "-" + spinq.ResetStyle + " Scenario: Skipped Scenario", " " + spinq.Gray + "-" + spinq.ResetStyle + " SKIPPED in 100ms | Scenarios: 1" + spinq.Gray + "-" + spinq.ResetStyle + " | 0 requests sent"},
		},
		{
			name: "Show NoSkip filters Skipped",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.NoSkip},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: event.Unset,
				},
				{
					Name:   "Success Scenario",
					Status: event.Success,
				},
			},
			wantOutput: []string{" ✓ Scenario: Success Scenario"},
		},
		{
			name: "Success with skipped scenario prints PASSED",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: event.Unset,
				},
				{
					Name:   "Success Scenario",
					Status: event.Success,
				},
			},
			wantOutput: []string{" ✓ PASSED in 100ms | Scenarios: 1✓ 1- | 0 requests sent"},
		},
		{
			name: "Show Failures filters Success and Skipped",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.Failed},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Skipped Scenario",
					Status: event.Unset,
				},
				{
					Name:   "Success Scenario",
					Status: event.Success,
					RequestResults: []aggregator.RequestResult{
						{Name: "Req1", Status: event.Success},
						{Name: "Req2", Status: event.Unset},
					},
				},
				{
					Name:   "Failure Scenario",
					Status: event.Failure,
					RequestResults: []aggregator.RequestResult{
						{Name: "Req3", Status: event.Failure},
					},
				},
			},
			wantOutput: []string{" ✗ Scenario: Failure Scenario"},
		},
		{
			name: "Dump On Failure fields",
			cfg:  config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.All},
			results: []aggregator.ScenarioResult{
				{
					Name:   "Dump Scenario",
					Status: event.Failure,
					RequestResults: []aggregator.RequestResult{
						{
							Name:     "ReqDump",
							Status:   event.Failure,
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

			sourceCh := make(chan aggregator.ScenarioResult, len(tt.results))
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

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	outputBuf := bytes.NewBufferString("hello\nworld\n")
	sourceCh <- aggregator.ScenarioResult{
		Name:   "Print Scenario",
		Status: event.Failure,
		Output: outputBuf,
		RequestResults: []aggregator.RequestResult{
			{Name: "Req", Status: event.Failure},
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

func TestFmtDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"just under a microsecond", 999 * time.Nanosecond, "999ns"},
		{"exactly a microsecond", 1 * time.Microsecond, "1µs"},
		{"just under a millisecond", 999 * time.Microsecond, "999µs"},
		{"exactly a millisecond", 1 * time.Millisecond, "1ms"},
		{"just under a second", 999 * time.Millisecond, "999ms"},
		{"exactly a second", 1 * time.Second, "1.00s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fmtDuration(tt.d); got != tt.want {
				t.Errorf("fmtDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestReportTime(t *testing.T) {
	tests := []struct {
		name      string
		d         time.Duration
		wantWrite bool
	}{
		{"negative duration is a no-op", -1 * time.Nanosecond, false},
		{"zero duration is a no-op", 0, false},
		{"positive duration writes", 1 * time.Nanosecond, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			n, err := reportTime(buf, "Pre: %8s\n", tt.d)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantWrite {
				if n == 0 || buf.Len() == 0 {
					t.Errorf("expected reportTime to write for duration %v, wrote nothing", tt.d)
				}
			} else {
				if n != 0 || buf.Len() != 0 {
					t.Errorf("expected reportTime to be a no-op for duration %v, got n=%d buf=%q", tt.d, n, buf.String())
				}
			}
		})
	}
}
