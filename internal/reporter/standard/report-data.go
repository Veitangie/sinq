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
	abortedScenarios       int
	totalRequests          int
	ranRequests            int
	successfulRequests     int
	abortedRequests        int
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
	case runner.Unset:
		fallthrough
	case runner.Skipped:
		scenarioMark = rd.markSkipped
	case runner.Aborted:
		scenarioMark = rd.markAborted
		rd.ranScenarios += 1
		rd.abortedScenarios += 1
	case runner.Success:
		rd.ranScenarios += 1
		rd.successfulScenarios += 1
	default:
		scenarioMark = rd.markFail
		rd.ranScenarios += 1
	}

	scenarioTackOn := ""
	if rd.cfg.Verbose {
		scenarioTackOn = fmt.Sprintf(" [Started: %s]", result.StartedAt.Format("15:04:05.000"))
	}

	ranRequestsScenario := 0
	successfulRequestsScenario := 0
	abortedRequestsScenario := 0
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
		case runner.Aborted:
			abortedRequestsScenario += 1
			ranRequestsScenario += 1
		default:
		}
	}

	rd.ranRequests += ranRequestsScenario
	rd.successfulRequests += successfulRequestsScenario
	rd.abortedRequests += abortedRequestsScenario
	skippedRequestsScenario := len(result.RequestResults) - ranRequestsScenario
	failedRequestsScenario := ranRequestsScenario - successfulRequestsScenario - abortedRequestsScenario

	switch rd.cfg.Show {
	case config.NoSkip:
		if result.Status == runner.Unset || result.Status == runner.Skipped {
			return nil
		}
	case config.Failed:
		if result.Status != runner.Failure && result.Status != runner.Error {
			return nil
		}
	case config.All:
	}

	successfulString := mkStringSpaceRight(successfulRequestsScenario, rd.markSuccess)
	failedString := mkStringSpaceRight(failedRequestsScenario, rd.markFail)
	abortedString := mkStringSpaceRight(abortedRequestsScenario, rd.markAborted)
	skippedString := mkStringSpaceRight(skippedRequestsScenario, rd.markSkipped)

	_, err := fmt.Fprintf(rd.writer, " %s Scenario: %s (%s%s%s%sin %s)%s\n",
		scenarioMark,
		result.Name,
		successfulString,
		failedString,
		abortedString,
		skippedString,
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

	if !rd.cfg.Verbose && result.Status == runner.Success || result.Status == runner.Unset {
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

func mkStringSpaceRight(num int, mark string) string {
	if num == 0 {
		return ""
	}
	return fmt.Sprintf("%d%s ", num, mark)
}

func mkStringSpaceLeft(num int, mark string) string {
	if num == 0 {
		return ""
	}
	return fmt.Sprintf(" %d%s", num, mark)
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
	case runner.Unset:
		fallthrough
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
	missingScenarios := (rd.size - rd.totalScenarios)
	abortedScenarios := rd.abortedScenarios + missingScenarios
	skippedScenarios := rd.size - rd.ranScenarios - missingScenarios
	failedScenarios := rd.ranScenarios - rd.successfulScenarios - rd.abortedScenarios

	if failedScenarios != 0 {
		finalMark = rd.markFail
		statusText = "FAILED"
	}

	if abortedScenarios != 0 {
		finalMark = rd.markAborted
		statusText = "ABORTED"
	}

	if skippedScenarios == rd.size {
		finalMark = rd.markSkipped
		statusText = "SKIPPED"
	}

	successfulString := mkStringSpaceLeft(rd.successfulScenarios, rd.markSuccess)
	failedString := mkStringSpaceLeft(failedScenarios, rd.markFail)
	abortedString := mkStringSpaceLeft(abortedScenarios, rd.markAborted)
	skippedString := mkStringSpaceLeft(skippedScenarios, rd.markSkipped)

	_, err := fmt.Fprintf(rd.writer, "\n %s %s in %s | Scenarios:%s%s%s%s | %d requests sent\n",
		finalMark,
		statusText,
		fmtDuration(duration),
		successfulString,
		failedString,
		abortedString,
		skippedString,
		rd.ranRequests,
	)
	return err
}
