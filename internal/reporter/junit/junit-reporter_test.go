// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package junit

import (
	"bytes"
	"encoding/xml"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/runner"
)

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
