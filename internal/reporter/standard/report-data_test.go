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

type substringErrorWriter struct {
	trigger string
	buf     bytes.Buffer
}

func (w *substringErrorWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.trigger) {
		return 0, errors.New("simulated error containing: " + w.trigger)
	}
	return w.buf.Write(p)
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

func TestReportData_ReportEnd_TallyArithmetic(t *testing.T) {
	buf := &bytes.Buffer{}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, buf, 20)
	rd.totalScenarios = 15
	rd.ranScenarios = 9
	rd.successfulScenarios = 2
	rd.abortedScenarios = 3

	if err := rd.reportEnd(time.Second); err != nil {
		t.Fatalf("reportEnd failed: %v", err)
	}

	out := buf.String()
	want := "Scenarios: 2✓ 4✗ 8○ 6-"
	if !strings.Contains(out, want) {
		t.Errorf("expected tally %q in output, got:\n%s", want, out)
	}
}

func TestReportData_RequestTallyArithmetic(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Show: config.All}, buf)

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- runner.ScenarioResult{
		Name:          "Tally Scenario",
		Status:        runner.Failure,
		TotalDuration: time.Millisecond,
		RequestResults: []runner.RequestResult{
			{Name: "R1", Status: runner.Success},
			{Name: "R2", Status: runner.Error},
			{Name: "R3", Status: runner.Aborted},
		},
	}
	close(sourceCh)
	timerCh <- time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("Report failed: %v", err)
	}

	out := buf.String()
	want := "(1✓ 1✗ 1○ in"
	if !strings.Contains(out, want) {
		t.Errorf("expected request tally %q in output, got:\n%s", want, out)
	}
}

func TestReportData_ShowFailed_Filtering(t *testing.T) {
	tests := []struct {
		name      string
		status    runner.ResultStatus
		wantShown bool
	}{
		{"Success not shown", runner.Success, false},
		{"Skipped not shown", runner.Unset, false},
		{"Aborted not shown", runner.Aborted, false},
		{"Failure shown", runner.Failure, true},
		{"Error shown", runner.Error, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			rep := NewReporter(config.ReporterConfig{Color: config.Never, Show: config.Failed}, buf)

			sourceCh := make(chan runner.ScenarioResult, 1)
			timerCh := make(chan time.Duration, 1)
			sourceCh <- runner.ScenarioResult{
				Name:   "FilterScenario",
				Status: tt.status,
			}
			close(sourceCh)
			timerCh <- time.Millisecond
			close(timerCh)

			if err := rep.Report(sourceCh, timerCh, 1); err != nil {
				t.Fatalf("Report failed: %v", err)
			}

			shown := strings.Contains(buf.String(), "Scenario: FilterScenario")
			if shown != tt.wantShown {
				t.Errorf("status %v: expected shown=%v, got shown=%v\noutput:\n%s", tt.status, tt.wantShown, shown, buf.String())
			}
		})
	}
}

func TestReportData_ReportRequest_NoFailedAssertions(t *testing.T) {
	buf := &bytes.Buffer{}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, buf, 1)

	err := rd.reportRequest(runner.RequestResult{Name: "Req", Status: runner.Success})
	if err != nil {
		t.Fatalf("reportRequest failed: %v", err)
	}

	if strings.Contains(buf.String(), "Failed assertions") {
		t.Errorf("expected no 'Failed assertions' line when there are none, got:\n%s", buf.String())
	}
}

func TestReportData_ReportOutput_CopyErrorPropagates(t *testing.T) {
	w := &substringErrorWriter{trigger: "OUTPUT_MARKER"}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)

	result := runner.ScenarioResult{
		Output: bytes.NewBufferString("OUTPUT_MARKER content\n"),
	}

	if err := rd.reportOutput(result); err == nil {
		t.Fatal("expected reportOutput to propagate the io.Copy error, got nil")
	}
}

func TestReportData_ReportOutput_ResetWriteErrorPropagates(t *testing.T) {
	const marker = "RESET_MARKER_XYZ"
	w := &substringErrorWriter{trigger: marker}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)
	rd.reset = marker

	result := runner.ScenarioResult{
		Output: bytes.NewBufferString("harmless content\n"),
	}

	if err := rd.reportOutput(result); err == nil {
		t.Fatal("expected reportOutput to propagate the reset-write error, got nil")
	}
}

func TestReportData_Report_RequestLoopErrorStopsIteration(t *testing.T) {
	const marker = "REQ_A_FAIL_MARKER"
	w := &substringErrorWriter{trigger: marker}
	rd := newReportData(config.ReporterConfig{Color: config.Never, Verbose: true, Show: config.All}, w, 1)

	result := runner.ScenarioResult{
		Name:   "LoopScenario",
		Status: runner.Success,
		RequestResults: []runner.RequestResult{
			{Name: marker, Status: runner.Success},
			{Name: "ReqB", Status: runner.Success},
		},
	}

	if err := rd.report(result); err == nil {
		t.Fatal("expected report to propagate the request-write error, got nil")
	}
	if strings.Contains(w.buf.String(), "ReqB") {
		t.Errorf("expected iteration to stop after the failing request, but ReqB was still written:\n%s", w.buf.String())
	}
}

func TestReportData_ReportRequest_ErrorMessageWriteErrorPropagates(t *testing.T) {
	const marker = "ERR_MSG_MARKER"
	w := &substringErrorWriter{trigger: marker}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)

	err := rd.reportRequest(runner.RequestResult{
		Name:         "Req",
		Status:       runner.Failure,
		ErrorMessage: marker,
	})
	if err == nil {
		t.Fatal("expected reportRequest to propagate the error-message write error, got nil")
	}
}

func TestReportData_ReportRequest_DurationsWriteErrorPropagates(t *testing.T) {
	w := &substringErrorWriter{trigger: "Pre:"}
	rd := newReportData(config.ReporterConfig{Color: config.Never, Verbose: true}, w, 1)

	err := rd.reportRequest(runner.RequestResult{
		Name:   "Req",
		Status: runner.Success,
		Pre:    time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected reportRequest to propagate the durations write error, got nil")
	}
}

func TestReportData_ReportRequest_ResponseWriteErrorPropagates(t *testing.T) {
	const marker = "RESPONSE_MARKER"
	w := &substringErrorWriter{trigger: marker}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)

	err := rd.reportRequest(runner.RequestResult{
		Name:     "Req",
		Status:   runner.Success,
		Response: marker,
	})
	if err == nil {
		t.Fatal("expected reportRequest to propagate the response write error, got nil")
	}
}
