// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package junit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/runner"
)

type errorWriter struct{}

func (e errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated write error")
}

func TestJUnitReporter_EscapingAndControlCharacters(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	nastyAssertion := "Expected 200 <script>alert(1)</script> & got 500 \x1b[31m[RED]\x1b[0m \x00"
	nastyError := "Fatal \x0b crash"

	sourceCh <- runner.ScenarioResult{
		Name:          "Nasty XML Test",
		StartedAt:     time.Now(),
		TotalDuration: 1 * time.Second,
		Status:        runner.Failure,
		RequestResults: []runner.RequestResult{
			{
				Name:             "NastyRequest",
				Status:           runner.Failure,
				FailedAssertions: []string{nastyAssertion},
				ErrorMessage:     nastyError,
			},
		},
	}

	close(sourceCh)
	timerCh <- 1 * time.Second
	close(timerCh)

	err := rep.Report(sourceCh, timerCh, 1)
	if err != nil {
		t.Fatalf("Failed to execute Report(): %v", err)
	}

	var parsedReport JUnitReport
	err = xml.Unmarshal(buf.Bytes(), &parsedReport)
	if err != nil {
		t.Fatalf("JUnit XML generation produced invalid XML 1.0!\nUnmarshal Error: %v\n\nGenerated XML:\n%s", err, buf.String())
	}

	if len(parsedReport.Suites) == 0 || len(parsedReport.Suites[0].TestCases) == 0 {
		t.Fatal("Parsed report is missing expected suites/testcases")
	}
}

func TestJUnitReporter_Comprehensive(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	outputBuf := bytes.NewBufferString("lua output")
	sourceCh <- runner.ScenarioResult{
		Name:          "Comprehensive",
		Tags:          []string{"api", "fast"},
		StartedAt:     time.Now(),
		TotalDuration: 1 * time.Second,
		Status:        runner.Failure,
		Output:        outputBuf,
		RequestResults: []runner.RequestResult{
			{Name: "SuccessReq", Status: runner.Success},
			{Name: "ErrorReq", Status: runner.Error, ErrorMessage: "runtime err"},
			{Name: "FailReq", Status: runner.Failure, FailedAssertions: []string{"bad body"}},
			{Name: "SkipReq", Status: runner.Unset},
			{Name: "AbortReq", Status: runner.Aborted},
		},
	}

	close(sourceCh)
	timerCh <- 1 * time.Second
	close(timerCh)

	err := rep.Report(sourceCh, timerCh, 1)
	if err != nil {
		t.Fatalf("Failed to execute Report(): %v", err)
	}

	var parsedReport JUnitReport
	err = xml.Unmarshal(buf.Bytes(), &parsedReport)
	if err != nil {
		t.Fatalf("Failed to unmarshal XML: %v\n%s", err, buf.String())
	}

	if len(parsedReport.Suites) != 1 {
		t.Fatalf("Expected 1 suite, got %d", len(parsedReport.Suites))
	}

	suite := parsedReport.Suites[0]
	if len(suite.Properties) != 1 || suite.Properties[0].Name != "tags" || suite.Properties[0].Contents != "api, fast" {
		t.Errorf("Expected tags property, got %v", suite.Properties)
	}

	if suite.SystemOut == nil || suite.SystemOut.Contents != "lua output" {
		t.Errorf("Expected system-out 'lua output', got %v", suite.SystemOut)
	}

	if len(suite.TestCases) != 5 {
		t.Fatalf("Expected 5 test cases, got %d", len(suite.TestCases))
	}

	if suite.TestCases[1].Error == nil || suite.TestCases[1].Error.Message != "runtime err" {
		t.Errorf("Expected error on second request, got %v", suite.TestCases[1].Error)
	}

	if suite.TestCases[3].Skipped == nil {
		t.Errorf("Expected SkippedReq to be skipped")
	}
	if suite.TestCases[4].Error == nil || suite.TestCases[4].Error.Message != "Request was interrupted" {
		t.Errorf("Expected AbortReq to report an interruption error, got %v", suite.TestCases[4].Error)
	}
}

func TestJUnitReporter_WriteErrors(t *testing.T) {
	rep := NewReporter(errorWriter{})

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- runner.ScenarioResult{Name: "ErrTest"}
	close(sourceCh)
	timerCh <- 1 * time.Second
	close(timerCh)

	err := rep.Report(sourceCh, timerCh, 1)
	if err == nil {
		t.Errorf("Expected an error from simulated write failures, got nil")
	}
}
