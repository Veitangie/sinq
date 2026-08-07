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
	"github.com/Veitangie/sinq/internal/runner"
	"github.com/Veitangie/sinq/internal/ui"
)

type reportData struct {
	cfg                    config.ReporterConfig
	markSuccess            string
	markFail               string
	markAborted            string
	markSkipped            string
	cyan                   string
	magenta                string
	lightGray              string
	reset                  string
	size                   int
	totalScenarios         int
	ranScenarios           int
	successfulScenarios    int
	totalRequests          int
	ranRequests            int
	successfulRequests     int
	prefixedScenarioWriter *prefixedWriter
	prefixedRequestWriter  *prefixedWriter
	prefixedVerboseWriter  *prefixedWriter
	writer                 io.Writer
}

func newReportData(cfg config.ReporterConfig, writer io.Writer, size int) *reportData {
	result := reportData{
		cfg:         cfg,
		markSuccess: checkmark,
		markFail:    cross,
		markAborted: circle,
		markSkipped: dash,
		size:        size,
		writer:      writer,
	}

	if cfg.Color != config.Never {
		result.markSuccess = ui.Green + result.markSuccess + ui.Reset
		result.markFail = ui.Red + result.markFail + ui.Reset
		result.markAborted = ui.Yellow + result.markAborted + ui.Reset
		result.markSkipped = ui.Gray + result.markSkipped + ui.Reset
		result.cyan = ui.Cyan
		result.magenta = ui.Magenta
		result.lightGray = ui.LightGray
		result.reset = ui.Reset
	}
	result.prefixedScenarioWriter = &prefixedWriter{prefix: fmt.Appendf(nil, "   %s┃%s ", result.magenta, result.reset), underlying: writer}
	result.prefixedRequestWriter = &prefixedWriter{prefix: fmt.Appendf(nil, "     %s┃%s ", result.cyan, result.reset), underlying: writer}
	result.prefixedVerboseWriter = &prefixedWriter{prefix: fmt.Appendf(nil, "     %s┃%s ", result.lightGray, result.reset), underlying: writer}

	return &result
}

func (rd *reportData) report(result runner.ScenarioResult) error {
	rd.totalScenarios += 1

	scenarioMark := rd.markSuccess
	switch result.Status {
	case runner.Skipped:
		scenarioMark = rd.markSkipped
	case runner.Aborted:
		scenarioMark = rd.markAborted
	case runner.Success:
		rd.successfulScenarios += 1
		rd.ranScenarios += 1
	default:
		scenarioMark = rd.markFail
		rd.ranScenarios += 1
	}

	switch rd.cfg.Show {
	case config.NoSkip:
		if result.Status == runner.Skipped {
			return nil
		}
	case config.Failed:
		if result.Status != runner.Failure && result.Status != runner.Error {
			for _, request := range result.RequestResults {
				if request.Status != runner.Skipped && request.Status != runner.Aborted {
					rd.ranRequests += 1
				}

				if request.Status == runner.Success {
					rd.successfulRequests += 1
				}
			}
			return nil
		}
	case config.All:
	}

	scenarioTackOn := ""
	if rd.cfg.Verbose {
		scenarioTackOn = fmt.Sprintf(" [Started: %s]", result.StartedAt.Format("15:04:05.000"))
	}

	ranRequestsScenario := 0
	successfulRequestsScenario := 0
	for _, request := range result.RequestResults {
		rd.totalRequests += 1
		switch request.Status {
		case runner.Success:
			successfulRequestsScenario += 1
			fallthrough
		case runner.Failure:
			fallthrough
		case runner.Error:
			ranRequestsScenario += 1
		default:
		}
	}
	rd.ranRequests += ranRequestsScenario
	rd.successfulRequests += successfulRequestsScenario

	_, err := fmt.Fprintf(rd.writer, " %s Scenario: %s (%d%s %d%s %d%s in %s)%s\n",
		scenarioMark,
		result.Name,
		successfulRequestsScenario, rd.markSuccess,
		ranRequestsScenario-successfulRequestsScenario, rd.markFail,
		len(result.RequestResults)-ranRequestsScenario, rd.markAborted,
		fmtDuration(result.TotalDuration),
		scenarioTackOn,
	)
	if err != nil {
		return err
	}

	err = rd.reportOutput(result)
	if err != nil {
		return err
	}

	if !rd.cfg.Verbose && result.Status == runner.Success || result.Status == runner.Skipped {
		return nil
	}

	for _, request := range result.RequestResults {
		err = rd.reportRequest(request)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rd *reportData) reportOutput(result runner.ScenarioResult) error {
	if result.Output != nil && result.Output.Len() != 0 {
		_, err := fmt.Fprint(rd.writer, "   Output:\n")
		if err != nil {
			return err
		}
		_, err = io.Copy(rd.prefixedScenarioWriter, result.Output)
		if err != nil {
			return err
		}
		_, err = io.WriteString(rd.writer, rd.reset)
		if err != nil {
			return err
		}
	}

	return nil
}

func (rd *reportData) reportRequest(request runner.RequestResult) error {
	requestMark := rd.markSuccess
	switch request.Status {
	case runner.Skipped:
		requestMark = rd.markSkipped
	case runner.Aborted:
		requestMark = rd.markAborted
	case runner.Success:
	default:
		requestMark = rd.markFail
	}

	requestTackOn := ""
	if rd.cfg.Verbose {
		requestTackOn = fmt.Sprintf(" [Started: %s]", request.StartedAt.Format("15:04:05.000"))
	}

	_, err := fmt.Fprintf(rd.writer, "   %s %s (%s)%s\n", requestMark, request.Name, fmtDuration(request.Total), requestTackOn)
	if err != nil {
		return err
	}

	if len(request.FailedAssertions) > 0 {
		_, err = fmt.Fprintf(rd.writer, "     %s Failed assertions: %s\n", rd.markFail, strings.Join(request.FailedAssertions, ", "))

		if err != nil {
			return err
		}
	}
	if err == nil && request.ErrorMessage != "" {
		_, err = fmt.Fprintf(rd.writer, "     %s Error: %s\n", rd.markFail, request.ErrorMessage)
		if err != nil {
			return err
		}
	}

	if rd.cfg.Verbose {
		err = rd.reportDurations(request)
		if err != nil {
			return err
		}
	}

	if request.Request != "" {
		_, err = fmt.Fprint(rd.writer, "     Request:\n")
		if err != nil {
			return err
		}
		_, err = io.WriteString(rd.prefixedRequestWriter, request.Request)
		if err != nil {
			return err
		}
	}

	if request.Response != "" {
		_, err = fmt.Fprint(rd.writer, "     Response:\n")
		if err != nil {
			return err
		}
		_, err = io.WriteString(rd.prefixedRequestWriter, request.Response)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rd *reportData) reportDurations(request runner.RequestResult) error {
	_, err := reportTime(rd.prefixedVerboseWriter, "Pre:    %8s\n", request.Pre)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Mat:    %8s\n", request.Materialization)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Parse:  %8s\n", request.Parsing)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Exec:   %8s\n", request.Execution)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Retry:  %8s\n", request.Retry)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Assert: %8s\n", request.Assert)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Post:   %8s\n", request.Post)
	return err
}

func (rd *reportData) reportEnd(duration time.Duration) error {
	finalMark := rd.markSuccess
	statusText := "PASSED"

	if rd.successfulScenarios != rd.ranScenarios {
		finalMark = rd.markFail
		statusText = "FAILED"
	}
	skippedScenarios := rd.size - rd.ranScenarios
	failedScenarios := rd.size - rd.successfulScenarios - skippedScenarios
	_, err := fmt.Fprintf(rd.writer, "\n %s %s in %s | Scenarios: %d%s %d%s %d%s (%d) | %d requests sent\n",
		finalMark,
		statusText,
		fmtDuration(duration),
		rd.successfulScenarios, rd.markSuccess,
		failedScenarios, rd.markFail,
		skippedScenarios, rd.markAborted,
		rd.size,
		rd.ranRequests,
	)
	return err
}
