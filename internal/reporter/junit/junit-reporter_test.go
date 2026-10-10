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
	if len(suite.Properties.Properties) != 1 || suite.Properties.Properties[0].Name != "tags" || suite.Properties.Properties[0].Value != "api, fast" {
		t.Errorf("Expected tags property, got %v", suite.Properties)
	}

	if suite.SystemOut == nil || suite.SystemOut.Contents != "lua output" {
		t.Errorf("Expected system-out 'lua output', got %v", suite.SystemOut)
	}

	if len(suite.TestCases) != 5 {
		t.Fatalf("Expected 5 test cases, got %d", len(suite.TestCases))
	}

	if suite.TestCases[0].Failure != nil || suite.TestCases[0].Error != nil || suite.TestCases[0].Skipped != nil {
		t.Errorf("Expected SuccessReq to have no failure/error/skipped, got %+v", suite.TestCases[0])
	}

	if suite.TestCases[1].Error == nil || suite.TestCases[1].Error.Message != "runtime err" {
		t.Errorf("Expected error on second request, got %v", suite.TestCases[1].Error)
	}

	if suite.TestCases[2].Failure == nil || suite.TestCases[2].Failure.Message != "bad body" {
		t.Errorf("Expected FailReq to report a failure with message 'bad body', got %v", suite.TestCases[2].Failure)
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

func TestJUnitReporter_TagsUseStandardPropertiesMarkup(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan runner.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- runner.ScenarioResult{
		Name:           "Tagged",
		Tags:           []string{"api", "fast"},
		StartedAt:      time.Now(),
		TotalDuration:  1 * time.Second,
		Status:         runner.Success,
		RequestResults: []runner.RequestResult{{Name: "Req", Status: runner.Success}},
	}
	close(sourceCh)
	timerCh <- 1 * time.Second
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("Failed to execute Report(): %v", err)
	}

	var parsed struct {
		Suites []struct {
			Properties []struct {
				Property []struct {
					Name  string `xml:"name,attr"`
					Value string `xml:"value,attr"`
				} `xml:"property"`
			} `xml:"properties"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal XML: %v\n%s", err, buf.String())
	}
	if len(parsed.Suites) != 1 {
		t.Fatalf("Expected 1 suite, got %d\n%s", len(parsed.Suites), buf.String())
	}

	props := parsed.Suites[0].Properties
	if len(props) != 1 {
		t.Fatalf("Expected exactly one <properties> element, got %d\n%s", len(props), buf.String())
	}
	if len(props[0].Property) != 1 || props[0].Property[0].Name != "tags" || props[0].Property[0].Value != "api, fast" {
		t.Errorf("Expected <properties><property name=\"tags\" value=\"api, fast\"/></properties>, got %+v\n%s", props[0].Property, buf.String())
	}

	dec := xml.NewDecoder(bytes.NewReader(buf.Bytes()))
	seenProperties := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "properties":
			seenProperties = true
		case "testcase":
			if !seenProperties {
				t.Fatalf("Expected <properties> to precede <testcase> elements\n%s", buf.String())
			}
		}
	}
}

func TestJUnitReporter_NoTagsOmitsProperties(t *testing.T) {
	tests := []struct {
		name string
		tags []string
	}{
		{name: "nil tags", tags: nil},
		{name: "empty tags", tags: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			rep := NewReporter(buf)

			sourceCh := make(chan runner.ScenarioResult, 1)
			timerCh := make(chan time.Duration, 1)

			sourceCh <- runner.ScenarioResult{
				Name:           "Untagged",
				Tags:           tt.tags,
				StartedAt:      time.Now(),
				TotalDuration:  1 * time.Second,
				Status:         runner.Success,
				RequestResults: []runner.RequestResult{{Name: "Req", Status: runner.Success}},
			}
			close(sourceCh)
			timerCh <- 1 * time.Second
			close(timerCh)

			if err := rep.Report(sourceCh, timerCh, 1); err != nil {
				t.Fatalf("Failed to execute Report(): %v", err)
			}

			dec := xml.NewDecoder(bytes.NewReader(buf.Bytes()))
			for {
				tok, err := dec.Token()
				if err != nil {
					break
				}
				if start, ok := tok.(xml.StartElement); ok && (start.Name.Local == "properties" || start.Name.Local == "property") {
					t.Fatalf("Expected no <%s> element for a scenario without tags\n%s", start.Name.Local, buf.String())
				}
			}
		})
	}
}
