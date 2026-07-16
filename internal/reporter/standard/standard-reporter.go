// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Veitangie/sinq/internal/config"
	"github.com/Veitangie/sinq/internal/reporter"
	"github.com/Veitangie/sinq/internal/runner"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Gray   = "\033[90m"
)

const (
	checkmark = "✓"
	cross     = "✗"
	circle    = "○"
	dash      = "-"
)

type StandardReporter struct {
	cfg    config.ReporterConfig
	writer io.Writer
}

var _ reporter.Reporter = StandardReporter{}

func NewReporter(cfg config.ReporterConfig, writer io.Writer) StandardReporter {
	return StandardReporter{cfg, writer}
}

func (r StandardReporter) Report(source <-chan runner.ScenarioResult, timer <-chan time.Duration, size int) error {
	var err error

	markSuccess := checkmark
	markFail := cross
	markAborted := circle
	markSkipped := dash

	if r.cfg.Color != config.Never {
		markSuccess = Green + markSuccess + Reset
		markFail = Red + markFail + Reset
		markAborted = Yellow + markAborted + Reset
		markSkipped = Gray + markSkipped + Reset
	}

	totalScenarios := 0
	ranScenarios := 0
	successfulScenarios := 0
	totalRequests := 0
	ranRequests := 0
	successfulRequests := 0
	for result := range source {
		totalScenarios += 1
		switch r.cfg.Show {
		case config.NoSkip:
			if result.Status == runner.Skipped {
				continue
			}
		case config.Failures:
			if result.Status != runner.Failure && result.Status != runner.Error {
				continue
			}
		case config.All:
		}

		scenarioMark := markSuccess
		switch result.Status {
		case runner.Skipped:
			scenarioMark = markSkipped
		case runner.Aborted:
			scenarioMark = markAborted
		case runner.Success:
			successfulScenarios += 1
			ranScenarios += 1
		default:
			scenarioMark = markFail
			ranScenarios += 1
		}

		scenarioTackOn := ""
		if r.cfg.Verbose {
			scenarioTackOn = fmt.Sprintf(" [Started: %s]", result.StartedAt.Format("15:04:05.000"))
		}

		_, err = fmt.Fprintf(r.writer, " %s Scenario: %s (%s)%s\n", scenarioMark, result.Name, fmtDuration(result.TotalDuration), scenarioTackOn)
		if err != nil {
			continue
		}

		for _, request := range result.RequestResults {
			totalRequests += 1
			if err != nil {
				continue
			}
			requestMark := markSuccess
			switch request.Status {
			case runner.Skipped:
				requestMark = markSkipped
			case runner.Aborted:
				requestMark = markAborted
			case runner.Success:
				successfulRequests += 1
				ranRequests += 1
			default:
				requestMark = markFail
				ranRequests += 1
			}

			requestTackOn := ""
			if r.cfg.Verbose {
				requestTackOn = fmt.Sprintf(" [Started: %s]", request.StartedAt.Format("15:04:05.000"))
			}

			_, err = fmt.Fprintf(r.writer, "   %s %s (%s)%s\n", requestMark, request.Name, fmtDuration(request.Total), requestTackOn)
			if err != nil {
				continue
			}
			if r.cfg.Verbose {
				_, err = reportTime(r.writer, "     - Pre:    %8s\n", request.Pre)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "     - Mat:    %8s\n", request.Materialization)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "     - Parse:  %8s\n", request.Parsing)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "     - Exec:   %8s\n", request.Execution)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "     - Retry:  %8s\n", request.Retry)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "     - Assert: %8s\n", request.Assert)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "     - Post:   %8s\n", request.Post)
				if err != nil {
					continue
				}
			}
			if err == nil && len(request.FailedAssertions) > 0 {
				_, err = fmt.Fprintf(r.writer, "   - Failed assertions: %s\n", strings.Join(request.FailedAssertions, ", "))

				if err != nil {
					continue
				}
			}
			if err == nil && request.ErrorMessage != "" {
				_, err = fmt.Fprintf(r.writer, "   - Error: %s\n", request.ErrorMessage)
				if err != nil {
					continue
				}
			}
			if request.Request != "" {
				_, err = fmt.Fprintf(r.writer, "Request:\n%s\n", request.Request)
				if err != nil {
					continue
				}
			}
			if request.Response != "" {
				_, err = fmt.Fprintf(r.writer, "Response:\n%s\n", request.Response)
				if err != nil {
					continue
				}
			}
		}

		_, err = r.writer.Write([]byte{'\n'})
		if err != nil {
			continue
		}
	}

	if err == nil {
		finalMark := markSuccess
		statusText := "PASSED"

		if successfulScenarios != ranScenarios {
			finalMark = markFail
			statusText = "FAILED"
		}
		skippedScenarios := size - ranScenarios
		failedScenarios := size - successfulScenarios - skippedScenarios
		_, err = fmt.Fprintf(r.writer, " %s %s in %s | Scenarios: %d%s %d%s %d%s (%d) | %d requests sent\n",
			finalMark,
			statusText,
			fmtDuration(<-timer),
			successfulScenarios, markSuccess,
			failedScenarios, markFail,
			skippedScenarios, markAborted,
			size,
			ranRequests,
		)
	}
	return err
}

func reportTime(writer io.Writer, format string, duration time.Duration) (int, error) {
	if duration > 0 {
		return fmt.Fprintf(writer, format, fmtDuration(duration))
	}
	return 0, nil
}

func fmtDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}
