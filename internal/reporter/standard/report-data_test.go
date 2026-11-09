// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
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

		sourceCh := make(chan aggregator.ScenarioResult, 1)
		timerCh := make(chan time.Duration, 1)

		outBuf := bytes.NewBufferString("hello")
		sourceCh <- aggregator.ScenarioResult{
			Name:          "Error Test",
			Status:        event.Failure,
			Output:        outBuf,
			StartedAt:     time.Now(),
			TotalDuration: time.Second,
			StageResults: []aggregator.StageResult{
				{
					Name:             "ErrReq",
					Status:           event.Failure,
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

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- aggregator.ScenarioResult{
		Name:          "Error Scenario",
		Status:        event.Error,
		TotalDuration: 1 * time.Millisecond,
		StageResults: []aggregator.StageResult{
			{
				Name:         "ErrorReq",
				Status:       event.Error,
				ErrorMessage: "failed to parse",
			},
		},
	}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	_ = rep.Report(sourceCh, timerCh, 1)

	outStr := buf.String()
	expectedStr := " ✗ Error Scenario (1✗ in 1ms)"
	if !strings.Contains(outStr, expectedStr) {
		t.Errorf("Error request was not tallied as a failure (✗). Expected to contain %q\nGot:\n%s", expectedStr, outStr)
	}
}

func TestReportData_ShowNone(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.None}, buf)

	sourceCh := make(chan aggregator.ScenarioResult, 2)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- aggregator.ScenarioResult{
		Name:          "Passed Scenario",
		Status:        event.Success,
		TotalDuration: 1 * time.Millisecond,
		StageResults: []aggregator.StageResult{
			{
				Name:     "PassReq",
				Status:   event.Success,
				Attempts: 1,
			},
		},
	}
	sourceCh <- aggregator.ScenarioResult{
		Name:          "Failed Scenario",
		Status:        event.Failure,
		TotalDuration: 1 * time.Millisecond,
		StageResults: []aggregator.StageResult{
			{
				Name:         "FailReq",
				Status:       event.Failure,
				ErrorMessage: "failed",
				Attempts:     1,
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

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- aggregator.ScenarioResult{
		Name:          "Tally Scenario",
		Status:        event.Failure,
		TotalDuration: time.Millisecond,
		StageResults: []aggregator.StageResult{
			{Name: "R1", Status: event.Success},
			{Name: "R2", Status: event.Error},
			{Name: "R3", Status: event.Aborted},
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
		status    event.ResultStatus
		wantShown bool
	}{
		{"Success not shown", event.Success, false},
		{"Skipped not shown", event.Unset, false},
		{"Aborted not shown", event.Aborted, false},
		{"Failure shown", event.Failure, true},
		{"Error shown", event.Error, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			rep := NewReporter(config.ReporterConfig{Color: config.Never, Show: config.Failed}, buf)

			sourceCh := make(chan aggregator.ScenarioResult, 1)
			timerCh := make(chan time.Duration, 1)
			sourceCh <- aggregator.ScenarioResult{
				Name:   "FilterScenario",
				Status: tt.status,
			}
			close(sourceCh)
			timerCh <- time.Millisecond
			close(timerCh)

			if err := rep.Report(sourceCh, timerCh, 1); err != nil {
				t.Fatalf("Report failed: %v", err)
			}

			shown := strings.Contains(buf.String(), "FilterScenario")
			if shown != tt.wantShown {
				t.Errorf("status %v: expected shown=%v, got shown=%v\noutput:\n%s", tt.status, tt.wantShown, shown, buf.String())
			}
		})
	}
}

func TestReportData_ReportStage_NoFailedAssertions(t *testing.T) {
	buf := &bytes.Buffer{}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, buf, 1)

	err := rd.reportStage(aggregator.StageResult{Name: "Req", Status: event.Success})
	if err != nil {
		t.Fatalf("reportStage failed: %v", err)
	}

	if strings.Contains(buf.String(), "Failed assertions") {
		t.Errorf("expected no 'Failed assertions' line when there are none, got:\n%s", buf.String())
	}
}

func TestReportData_ReportOutput_CopyErrorPropagates(t *testing.T) {
	w := &substringErrorWriter{trigger: "OUTPUT_MARKER"}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)

	result := aggregator.ScenarioResult{
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

	result := aggregator.ScenarioResult{
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

	result := aggregator.ScenarioResult{
		Name:   "LoopScenario",
		Status: event.Success,
		StageResults: []aggregator.StageResult{
			{Name: marker, Status: event.Success},
			{Name: "ReqB", Status: event.Success},
		},
	}

	if err := rd.report(result); err == nil {
		t.Fatal("expected report to propagate the request-write error, got nil")
	}
	if strings.Contains(w.buf.String(), "ReqB") {
		t.Errorf("expected iteration to stop after the failing request, but ReqB was still written:\n%s", w.buf.String())
	}
}

func TestReportData_ReportStage_ErrorMessageWriteErrorPropagates(t *testing.T) {
	const marker = "ERR_MSG_MARKER"
	w := &substringErrorWriter{trigger: marker}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)

	err := rd.reportStage(aggregator.StageResult{
		Name:         "Req",
		Status:       event.Failure,
		ErrorMessage: marker,
	})
	if err == nil {
		t.Fatal("expected reportStage to propagate the error-message write error, got nil")
	}
}

func TestReportData_ReportStage_DurationsWriteErrorPropagates(t *testing.T) {
	w := &substringErrorWriter{trigger: "Pre:"}
	rd := newReportData(config.ReporterConfig{Color: config.Never, Verbose: true}, w, 1)

	err := rd.reportStage(aggregator.StageResult{
		Name:   "Req",
		Status: event.Success,
		Pre:    time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected reportStage to propagate the durations write error, got nil")
	}
}

func TestReportData_ReportStage_ResponseWriteErrorPropagates(t *testing.T) {
	const marker = "RESPONSE_MARKER"
	w := &substringErrorWriter{trigger: marker}
	rd := newReportData(config.ReporterConfig{Color: config.Never}, w, 1)

	err := rd.reportStage(aggregator.StageResult{
		Name:     "Req",
		Status:   event.Success,
		Response: marker,
	})
	if err == nil {
		t.Fatal("expected reportStage to propagate the response write error, got nil")
	}
}

func reportOne(t *testing.T, result aggregator.ScenarioResult) (out string) {
	t.Helper()
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Show: config.All}, buf)
	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)
	sourceCh <- result
	close(sourceCh)
	timerCh <- time.Millisecond
	close(timerCh)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Reporting panicked: %v\nOutput so far:\n%s", r, buf.String())
		}
	}()
	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("Report failed: %v", err)
	}
	return buf.String()
}

func TestReportData_LabelsFollowTheName(t *testing.T) {
	out := reportOne(t, aggregator.ScenarioResult{
		Name:          "Checkout",
		Labels:        []string{"admin", "card"},
		Status:        event.Success,
		TotalDuration: time.Millisecond,
		StageResults:  []aggregator.StageResult{{Name: "Pay", Status: event.Success, Attempts: 1}},
	})

	if !strings.Contains(out, "Checkout (admin, card) (") {
		t.Errorf("Expected the labels in parentheses after the name, got:\n%s", out)
	}
}

func TestReportData_RunNumberComesAfterTheLabels(t *testing.T) {
	out := reportOne(t, aggregator.ScenarioResult{
		Name:          "Checkout",
		Labels:        []string{"admin", "card"},
		CntId:         1,
		TotalRunCount: 2,
		Status:        event.Success,
		TotalDuration: time.Millisecond,
		StageResults:  []aggregator.StageResult{{Name: "Pay", Status: event.Success, Attempts: 1}},
	})

	if !strings.Contains(out, "Checkout (admin, card) #2 (") {
		t.Errorf("Expected the labels after the name and the run number last, got:\n%s", out)
	}
}

func TestReportData_RunNumberWithoutLabels(t *testing.T) {
	out := reportOne(t, aggregator.ScenarioResult{
		Name:          "Checkout",
		CntId:         2,
		TotalRunCount: 3,
		Status:        event.Success,
		TotalDuration: time.Millisecond,
		StageResults:  []aggregator.StageResult{{Name: "Pay", Status: event.Success, Attempts: 1}},
	})

	if !strings.Contains(out, "Checkout #3 (") {
		t.Errorf("Expected the run number, counting from 1, after the name, got:\n%s", out)
	}
}

func TestReportData_NoLabelsNoParentheses(t *testing.T) {
	out := reportOne(t, aggregator.ScenarioResult{
		Name:          "Checkout",
		Status:        event.Success,
		TotalDuration: time.Millisecond,
		StageResults:  []aggregator.StageResult{{Name: "Pay", Status: event.Success, Attempts: 1}},
	})

	if !strings.Contains(out, "Checkout (1") {
		t.Errorf("Expected only the tally after the name, got:\n%s", out)
	}
}

func TestReportData_RunNumberSuffix(t *testing.T) {
	cases := []struct {
		name     string
		cntId    int
		total    int
		wantRun  string
		wantNone bool
	}{
		{"second of three", 1, 3, "#2", false},
		{"first of two", 0, 2, "#1", false},
		{"single run", 0, 1, "", true},
		{"skipped", 0, 0, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := reportOne(t, aggregator.ScenarioResult{
				Name:          "Counted",
				CntId:         tc.cntId,
				TotalRunCount: tc.total,
				Status:        event.Success,
				TotalDuration: time.Millisecond,
				StageResults:  []aggregator.StageResult{{Name: "Req", Status: event.Success, Attempts: 1}},
			})

			var scenarioLine string
			for line := range strings.SplitSeq(out, "\n") {
				if strings.Contains(line, "Counted") {
					scenarioLine = line
				}
			}
			if tc.wantNone && strings.Contains(scenarioLine, "#") {
				t.Errorf("Expected no run number for a scenario that runs once, got %q", scenarioLine)
			}
			if !tc.wantNone && !strings.Contains(scenarioLine, tc.wantRun) {
				t.Errorf("Expected run number %s in the scenario line, got %q", tc.wantRun, scenarioLine)
			}
		})
	}
}

func TestReportData_RequestsSentPlural(t *testing.T) {
	cases := []struct {
		attempts int
		want     string
	}{
		{0, "| 0 requests sent\n"},
		{1, "| 1 request sent\n"},
		{2, "| 2 requests sent\n"},
		{11, "| 11 requests sent\n"},
		{21, "| 21 requests sent\n"},
		{101, "| 101 requests sent\n"},
	}
	for _, tc := range cases {
		t.Run(strconv.Itoa(tc.attempts), func(t *testing.T) {
			buf := &bytes.Buffer{}
			rep := NewReporter(config.ReporterConfig{Color: config.Never, Show: config.None}, buf)

			sourceCh := make(chan aggregator.ScenarioResult, 1)
			timerCh := make(chan time.Duration, 1)
			sourceCh <- aggregator.ScenarioResult{
				Name:          "Counted",
				Status:        event.Success,
				TotalDuration: time.Millisecond,
				StageResults:  []aggregator.StageResult{{Name: "Request", Status: event.Success, Attempts: tc.attempts}},
			}
			close(sourceCh)
			timerCh <- time.Millisecond
			close(timerCh)

			_ = rep.Report(sourceCh, timerCh, 1)

			if !strings.HasSuffix(buf.String(), tc.want) {
				t.Errorf("Expected the summary to end with %q, got:\n%s", tc.want, buf.String())
			}
		})
	}
}

func TestReportData_RequestsSentSumsAttempts(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(config.ReporterConfig{Color: config.Never, Verbose: false, Show: config.None}, buf)

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- aggregator.ScenarioResult{
		Name:          "Mixed Scenario",
		Status:        event.Aborted,
		TotalDuration: 1 * time.Millisecond,
		StageResults: []aggregator.StageResult{
			{Name: "Repeated", Status: event.Success, Attempts: 3},
			{Name: "Failed", Status: event.Failure, Attempts: 2},
			{Name: "Errored", Status: event.Error, Attempts: 1},
			{Name: "Interrupted", Status: event.Aborted, Attempts: 2},
			{Name: "NeverRan", Status: event.Unset},
		},
	}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	_ = rep.Report(sourceCh, timerCh, 1)

	expected := "| 8 requests sent\n"
	if !strings.Contains(buf.String(), expected) {
		t.Errorf("Expected every stage's attempts to be counted, including the interrupted one (%q). Got:\n%s", expected, buf.String())
	}
}
