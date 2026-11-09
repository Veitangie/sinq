// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package junit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
)

type errorWriter struct{}

func (e errorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated write error")
}

func TestJUnitReporter_EscapingAndControlCharacters(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	nastyAssertion := "Expected 200 <script>alert(1)</script> & got 500 \x1b[31m[RED]\x1b[0m \x00"
	nastyError := "Fatal \x0b crash"

	sourceCh <- aggregator.ScenarioResult{
		Name:          "Nasty XML Test",
		StartedAt:     time.Now(),
		TotalDuration: 1 * time.Second,
		Status:        event.Failure,
		StageResults: []aggregator.StageResult{
			{
				Name:             "NastyRequest",
				Status:           event.Failure,
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

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	outputBuf := bytes.NewBufferString("lua output")
	sourceCh <- aggregator.ScenarioResult{
		Name:          "Comprehensive",
		Tags:          []string{"api", "fast"},
		StartedAt:     time.Now(),
		TotalDuration: 1 * time.Second,
		Status:        event.Failure,
		Output:        outputBuf,
		StageResults: []aggregator.StageResult{
			{Name: "SuccessReq", Status: event.Success},
			{Name: "ErrorReq", Status: event.Error, ErrorMessage: "runtime err"},
			{Name: "FailReq", Status: event.Failure, FailedAssertions: []string{"bad body"}},
			{Name: "SkipReq", Status: event.Unset},
			{Name: "AbortReq", Status: event.Aborted},
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
	if suite.TestCases[4].Error == nil || suite.TestCases[4].Error.Message != "Stage was interrupted" {
		t.Errorf("Expected AbortReq to report an interruption error, got %v", suite.TestCases[4].Error)
	}
}

func TestJUnitReporter_WriteErrors(t *testing.T) {
	rep := NewReporter(errorWriter{})

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- aggregator.ScenarioResult{Name: "ErrTest"}
	close(sourceCh)
	timerCh <- 1 * time.Second
	close(timerCh)

	err := rep.Report(sourceCh, timerCh, 1)
	if err == nil {
		t.Errorf("Expected an error from simulated write failures, got nil")
	}
}

func TestJUnitReporter_RunsOfOneScenarioHaveDistinctNames(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	runs := []aggregator.ScenarioResult{
		{Name: "Checkout", Labels: []string{"admin", "card"}, CntId: 0, TotalRunCount: 2},
		{Name: "Checkout", Labels: []string{"admin", "card"}, CntId: 1, TotalRunCount: 2},
		{Name: "Checkout", Labels: []string{"guest", "card"}, CntId: 0, TotalRunCount: 2},
	}
	sourceCh := make(chan aggregator.ScenarioResult, len(runs))
	for _, r := range runs {
		r.Status = event.Success
		r.StartedAt = time.Now()
		r.TotalDuration = time.Millisecond
		r.StageResults = []aggregator.StageResult{{Name: "Pay", Status: event.Success}}
		sourceCh <- r
	}
	close(sourceCh)
	timerCh := make(chan time.Duration, 1)
	timerCh <- time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, len(runs)); err != nil {
		t.Fatalf("Failed to execute Report(): %v", err)
	}

	var parsed struct {
		Suites []struct {
			Name      string `xml:"name,attr"`
			TestCases []struct {
				Classname string `xml:"classname,attr"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Failed to unmarshal XML: %v\n%s", err, buf.String())
	}
	if len(parsed.Suites) != len(runs) {
		t.Fatalf("Expected %d suites, got %d\n%s", len(runs), len(parsed.Suites), buf.String())
	}

	seen := map[string]bool{}
	for i, s := range parsed.Suites {
		if seen[s.Name] {
			t.Errorf("Expected every run to have its own suite name, %q appears twice", s.Name)
		}
		seen[s.Name] = true
		for _, label := range runs[i].Labels {
			if !strings.Contains(s.Name, label) {
				t.Errorf("Expected suite %q to carry the label %q", s.Name, label)
			}
		}
		if want := "#" + strconv.Itoa(runs[i].CntId+1); !strings.Contains(s.Name, want) {
			t.Errorf("Expected suite %q to carry the run number %s", s.Name, want)
		}
		if len(s.TestCases) == 0 || s.TestCases[0].Classname != s.Name {
			t.Errorf("Expected the test cases' classname to match the suite name %q", s.Name)
		}
	}
}

func TestJUnitReporter_SingleRunHasNoRunNumber(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	sourceCh <- aggregator.ScenarioResult{Name: "Checkout", Status: event.Success, StartedAt: time.Now(), TotalRunCount: 1}
	close(sourceCh)
	timerCh := make(chan time.Duration, 1)
	timerCh <- time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("Failed to execute Report(): %v", err)
	}
	if strings.Contains(buf.String(), "#1") {
		t.Errorf("Expected a scenario that runs once to carry no run number, got\n%s", buf.String())
	}
}

func TestJUnitReporter_TestCaseCarriesItsFile(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	sourceCh <- aggregator.ScenarioResult{Name: "Checkout", Status: event.Success, StartedAt: time.Now(), TotalRunCount: 1,
		StageResults: []aggregator.StageResult{
			{Name: "Check out", Filename: "orders/03_checkout.sinq", Status: event.Success, Attempts: 1},
			{Name: "a.sinq", Filename: "a.sinq", Status: event.Success, Attempts: 1},
		}}
	close(sourceCh)
	timerCh := make(chan time.Duration, 1)
	timerCh <- time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("Failed to execute Report(): %v", err)
	}

	var parsed struct {
		Suites []struct {
			Cases []struct {
				Name string `xml:"name,attr"`
				File string `xml:"file,attr"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Expected valid XML, got %v\n%s", err, buf.String())
	}
	if len(parsed.Suites) != 1 || len(parsed.Suites[0].Cases) != 2 {
		t.Fatalf("Expected one suite with two test cases, got\n%s", buf.String())
	}
	cases := parsed.Suites[0].Cases
	if cases[0].Name != "Check out" || cases[0].File != "orders/03_checkout.sinq" {
		t.Errorf("Expected the named stage to name its file, got %+v", cases[0])
	}
	if cases[1].File != "a.sinq" {
		t.Errorf("Expected an unnamed stage to carry its file too, got %+v", cases[1])
	}
}

func TestJUnitReporter_TagsUseStandardPropertiesMarkup(t *testing.T) {
	buf := &bytes.Buffer{}
	rep := NewReporter(buf)

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)

	sourceCh <- aggregator.ScenarioResult{
		Name:          "Tagged",
		Tags:          []string{"api", "fast"},
		StartedAt:     time.Now(),
		TotalDuration: 1 * time.Second,
		Status:        event.Success,
		StageResults:  []aggregator.StageResult{{Name: "Req", Status: event.Success}},
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

func TestJUnitReporter_LabelsProperty(t *testing.T) {
	tests := []struct {
		name   string
		tags   []string
		labels []string
		want   map[string]string
	}{
		{"tags only", []string{"smoke"}, nil, map[string]string{"tags": "smoke"}},
		{"labels only", nil, []string{"admin", "card"}, map[string]string{"labels": "admin, card"}},
		{"tags and labels", []string{"smoke", "api"}, []string{"guest"}, map[string]string{"tags": "smoke, api", "labels": "guest"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			rep := NewReporter(buf)

			sourceCh := make(chan aggregator.ScenarioResult, 1)
			timerCh := make(chan time.Duration, 1)
			sourceCh <- aggregator.ScenarioResult{
				Name:          "Checkout",
				Tags:          tt.tags,
				Labels:        tt.labels,
				StartedAt:     time.Now(),
				TotalDuration: time.Second,
				Status:        event.Success,
				StageResults:  []aggregator.StageResult{{Name: "Req", Status: event.Success}},
			}
			close(sourceCh)
			timerCh <- time.Second
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
			if len(parsed.Suites) != 1 || len(parsed.Suites[0].Properties) != 1 {
				t.Fatalf("Expected one suite with one <properties> block\n%s", buf.String())
			}

			got := map[string]string{}
			for _, p := range parsed.Suites[0].Properties[0].Property {
				got[p.Name] = p.Value
			}
			if len(got) != len(tt.want) {
				t.Errorf("Expected properties %v, got %v", tt.want, got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("Expected property %s=%q, got %q", k, v, got[k])
				}
			}
		})
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

			sourceCh := make(chan aggregator.ScenarioResult, 1)
			timerCh := make(chan time.Duration, 1)

			sourceCh <- aggregator.ScenarioResult{
				Name:          "Untagged",
				Tags:          tt.tags,
				StartedAt:     time.Now(),
				TotalDuration: 1 * time.Second,
				Status:        event.Success,
				StageResults:  []aggregator.StageResult{{Name: "Req", Status: event.Success}},
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
