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
	"veitangie.dev/sinq/internal/runner"
)

type countdownErrorWriter struct {
	Count int
}

func (c *countdownErrorWriter) Write(p []byte) (n int, err error) {
	if c.Count <= 0 {
		return 0, errors.New("simulated error")
	}
	c.Count--
	return len(p), nil
}

func TestReportData_WriteErrors(t *testing.T) {
	for i := range 200 {
		writer := &countdownErrorWriter{Count: i}
		rep := NewReporter(config.ReporterConfig{Verbose: true, Color: config.Always}, writer)

		sourceCh := make(chan runner.ScenarioResult, 1)
		timerCh := make(chan time.Duration, 1)

		outBuf := bytes.NewBufferString("hello")
		sourceCh <- runner.ScenarioResult{
			Name:          "Error Test",
			Status:        runner.Failure,
			Output:        outBuf,
			StartedAt:     time.Now(),
			TotalDuration: time.Second,
			RequestResults: []runner.RequestResult{
				{
					Name:             "ErrReq",
					Status:           runner.Failure,
					FailedAssertions: []string{"assert1"},
					ErrorMessage:     "err msg",
					Request:          "GET /",
					Response:         "200 OK",
					Pre:              time.Millisecond,
					Materialization:  time.Millisecond,
					Parsing:          time.Millisecond,
					Execution:        time.Millisecond,
					Retry:            time.Millisecond,
					Assert:           time.Millisecond,
					Post:             time.Millisecond,
				},
			},
		}
		close(sourceCh)
		timerCh <- 1 * time.Millisecond
		close(timerCh)

		err := rep.Report(sourceCh, timerCh, 1)
		if err == nil {
			break
		}
	}
}

func TestReportData_ErrorRequestTallyingBug(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Verbose: false}, buf)

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- runner.ScenarioResult{
		Name:          "Error Scenario",
		Status:        runner.Error,
		TotalDuration: 1 * time.Millisecond,
		RequestResults: []runner.RequestResult{
			{
				Name:         "ErrorReq",
				Status:       runner.Error,
				ErrorMessage: "failed to parse",
			},
		},
	}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	_ = rep.Report(sourceCh, timerCh, 1)

	outStr := buf.String()
	expectedStr := " ✗ Scenario: Error Scenario (1✗ in 1ms)"
	if !strings.Contains(outStr, expectedStr) {
		t.Errorf("Error request was not tallied as a failure (✗). Expected to contain %q\nGot:\n%s", expectedStr, outStr)
	}
}

func TestReportData_ShowNone(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.None}, buf)

	sourceCh := make(chan runner.ScenarioResult, 2)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- runner.ScenarioResult{
		Name:          "Passed Scenario",
		Status:        runner.Success,
		TotalDuration: 1 * time.Millisecond,
		RequestResults: []runner.RequestResult{
			{
				Name:   "PassReq",
				Status: runner.Success,
			},
		},
	}
	sourceCh <- runner.ScenarioResult{
		Name:          "Failed Scenario",
		Status:        runner.Failure,
		TotalDuration: 1 * time.Millisecond,
		RequestResults: []runner.RequestResult{
			{
				Name:         "FailReq",
				Status:       runner.Failure,
				ErrorMessage: "failed",
			},
		},
	}
	close(sourceCh)
	timerCh <- 2 * time.Millisecond
	close(timerCh)

	_ = rep.Report(sourceCh, timerCh, 2)

	outStr := buf.String()
	if strings.Contains(outStr, "Scenario:") {
		t.Errorf("Expected no scenario details in Show None mode. Got:\n%s", outStr)
	}
	expectedEndStr := "✗ FAILED in 2ms | Scenarios: 1✓ 1✗ | 2 requests sent\n"
	if !strings.Contains(outStr, expectedEndStr) {
		t.Errorf("Expected end summary to be exactly %q in Show None mode. Got:\n%s", expectedEndStr, outStr)
	}
}
