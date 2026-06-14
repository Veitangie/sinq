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

	"github.com/Veitangie/sinq/internal/reporter"
	"github.com/Veitangie/sinq/internal/runner"
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
	Name      string          `xml:"name,attr"`
	Tests     int             `xml:"tests,attr"`
	Failures  int             `xml:"failures,attr"`
	Errors    int             `xml:"errors,attr"`
	Time      string          `xml:"time,attr"`
	Timestamp string          `xml:"timestamp,attr"`
	TestCases []JUnitTestCase `xml:"testcase"`
}

type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Error     *JUnitFailure `xml:"error,omitempty"`
}

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

func (r JUnitReporter) Report(source <-chan runner.ScenarioResult, timer <-chan time.Duration, size int) error {
	result := JUnitReport{
		Suites: make([]JUnitTestSuite, size),
	}
	curIdx := 0

	for scenarioResult := range source {
		suite := &result.Suites[curIdx]
		curIdx += 1

		suite.Name = scenarioResult.Name
		suite.Tests = len(scenarioResult.RequestResults)
		result.Tests += suite.Tests
		suite.Timestamp = scenarioResult.StartedAt.Format(time.RFC3339)
		suite.Time = fmt.Sprintf("%.3f", scenarioResult.TotalDuration.Seconds())
		suite.TestCases = make([]JUnitTestCase, 0, len(scenarioResult.RequestResults))

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
			if requestResult.Status == runner.Error {
				suite.Errors += 1
				suite.TestCases[len(suite.TestCases)-1].Error = &JUnitFailure{
					Message: requestResult.ErrorMessage,
					Type:    "RuntimeError",
				}
			}
		}
		result.Failures += suite.Failures
		result.Errors += suite.Errors

	}

	result.Time = fmt.Sprintf("%.3f", (<-timer).Seconds())
	result.Suites = result.Suites[:curIdx]
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
