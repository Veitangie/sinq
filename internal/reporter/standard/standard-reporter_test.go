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
			wantOutput: []string{"Scenario: Test Scenario", "Status: Success", "Req1", "Total tests: 1, successful: 1"},
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
			wantOutput: []string{Red, Reset, "ReqColor", "boom", "Total tests: 1, successful: 0"},
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
				"Pre script duration: 1ms",
				"Materialization duration: 2ms",
				"Parsing duration: 3ms",
				"Execution duration: 4ms",
				"Retry script duration: 5ms",
				"Assert script duration: 6ms",
				"Post script duration: 7ms",
				"Failed assertions: assert(200) failed, body check failed",
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
			wantOutput: []string{Yellow, "Aborted Scenario", "ReqA"},
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
