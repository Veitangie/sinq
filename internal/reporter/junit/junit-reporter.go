// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package junit

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/reporter"
)

type JUnitReport struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []JUnitTestSuite `xml:"testsuite"`
}

type JUnitTestSuite struct {
	Name       string           `xml:"name,attr"`
	Tests      int              `xml:"tests,attr"`
	Failures   int              `xml:"failures,attr"`
	Errors     int              `xml:"errors,attr"`
	Time       string           `xml:"time,attr"`
	Timestamp  string           `xml:"timestamp,attr"`
	Properties *JUnitProperties `xml:"properties,omitempty"`
	TestCases  []JUnitTestCase  `xml:"testcase"`
	SystemOut  *JUnitSystemOut  `xml:"system-out,omitempty"`
}

type JUnitSystemOut struct {
	Contents string `xml:",chardata"`
}

type JUnitProperties struct {
	Properties []JUnitProperty `xml:"property,omitempty"`
}

type JUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Error     *JUnitFailure `xml:"error,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

type JUnitSkipped struct{}

type JUnitFailure struct {
	Message  string `xml:"message,attr"`
	Type     string `xml:"type,attr"`
	Contents string `xml:",chardata"`
}

type JUnitReporter struct {
	writer io.Writer
}

var _ reporter.Reporter = JUnitReporter{}

func NewReporter(writer io.Writer) JUnitReporter {
	return JUnitReporter{writer}
}

func (r JUnitReporter) Report(source <-chan aggregator.ScenarioResult, timer <-chan time.Duration, size int) error {
	result := JUnitReport{
		Suites: make([]JUnitTestSuite, 0, size),
	}

	for scenarioResult := range source {
		result.Suites = append(result.Suites, JUnitTestSuite{})
		suite := &result.Suites[len(result.Suites)-1]

		suite.Name = suiteName(scenarioResult)
		suite.Tests = len(scenarioResult.RequestResults)
		result.Tests += suite.Tests
		suite.Timestamp = scenarioResult.StartedAt.Format(time.RFC3339)
		suite.Time = fmt.Sprintf("%.3f", scenarioResult.TotalDuration.Seconds())
		suite.TestCases = make([]JUnitTestCase, 0, len(scenarioResult.RequestResults))

		if len(scenarioResult.Tags) != 0 || len(scenarioResult.Labels) != 0 {
			suite.Properties = &JUnitProperties{make([]JUnitProperty, 0, 2)}
		}
		if len(scenarioResult.Tags) != 0 {
			suite.Properties.Properties = append(suite.Properties.Properties, JUnitProperty{Name: "tags", Value: strings.Join(scenarioResult.Tags, ", ")})
		}

		if len(scenarioResult.Labels) != 0 {
			suite.Properties.Properties = append(suite.Properties.Properties, JUnitProperty{Name: "labels", Value: strings.Join(scenarioResult.Labels, ", ")})
		}
		if scenarioResult.Output != nil && scenarioResult.Output.Len() != 0 {
			suite.SystemOut = &JUnitSystemOut{Contents: scenarioResult.Output.String()}
		}

		for _, requestResult := range scenarioResult.RequestResults {
			suite.TestCases = append(suite.TestCases, JUnitTestCase{
				Name:      requestResult.Name,
				Classname: suite.Name,
				Time:      fmt.Sprintf("%.3f", requestResult.Total.Seconds()),
			})
			if len(requestResult.FailedAssertions) > 0 {
				suite.Failures += 1
				suite.TestCases[len(suite.TestCases)-1].Failure = &JUnitFailure{
					Message: strings.Join(requestResult.FailedAssertions, ", "),
					Type:    "AssertionFailure",
				}
			}
			if requestResult.Status == event.Error {
				suite.Errors += 1
				suite.TestCases[len(suite.TestCases)-1].Error = &JUnitFailure{
					Message: requestResult.ErrorMessage,
					Type:    "RuntimeError",
				}
			}
			if requestResult.Status == event.Aborted {
				suite.Errors += 1
				suite.TestCases[len(suite.TestCases)-1].Error = &JUnitFailure{
					Message: "Request was interrupted",
					Type:    "RuntimeError",
				}
			}
			if requestResult.Status == event.Unset || requestResult.Status == event.Skipped {
				suite.TestCases[len(suite.TestCases)-1].Skipped = &JUnitSkipped{}
			}
		}
		result.Failures += suite.Failures
		result.Errors += suite.Errors

	}

	result.Time = fmt.Sprintf("%.3f", (<-timer).Seconds())
	toWrite, err := xml.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	_, err = r.writer.Write([]byte(xml.Header))
	if err != nil {
		return err
	}
	_, err = r.writer.Write(toWrite)
	return err
}

func suiteName(result aggregator.ScenarioResult) string {
	var name strings.Builder
	name.WriteString(result.Name)
	for _, label := range result.Labels {
		name.WriteByte('_')
		name.WriteString(label)
	}
	if result.TotalRunCount > 1 {
		fmt.Fprintf(&name, "#%d", result.CntId+1)
	}
	return name.String()
}
