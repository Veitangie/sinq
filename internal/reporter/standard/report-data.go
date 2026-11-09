// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/ui"
	"veitangie.dev/spinq"
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
	totalStages            int
	ranStages              int
	successfulStages       int
	abortedStages          int
	prefixedScenarioWriter *prefixedWriter
	prefixedStageWriter    *prefixedWriter
	prefixedVerboseWriter  *prefixedWriter
	writer                 io.Writer
	printed                bool
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
		result.markSuccess = spinq.Green + result.markSuccess + spinq.ResetStyle
		result.markFail = spinq.Red + result.markFail + spinq.ResetStyle
		result.markAborted = spinq.Yellow + result.markAborted + spinq.ResetStyle
		result.markSkipped = spinq.Gray + result.markSkipped + spinq.ResetStyle
		result.cyan = spinq.Cyan
		result.magenta = spinq.Magenta
		result.lightGray = ui.LightGray
		result.reset = spinq.ResetStyle
	}
	result.prefixedScenarioWriter = &prefixedWriter{prefix: fmt.Appendf(nil, "   %s┃%s ", result.magenta, result.reset), underlying: writer}
	result.prefixedStageWriter = &prefixedWriter{prefix: fmt.Appendf(nil, "     %s┃%s ", result.cyan, result.reset), underlying: writer}
	result.prefixedVerboseWriter = &prefixedWriter{prefix: fmt.Appendf(nil, "     %s┃%s ", result.lightGray, result.reset), underlying: writer}

	return &result
}

func (rd *reportData) report(result aggregator.ScenarioResult) error {
	rd.totalScenarios += 1

	scenarioMark := rd.markSuccess
	switch result.Status {
	case event.Unset:
		fallthrough
	case event.Skipped:
		scenarioMark = rd.markSkipped
	case event.Aborted:
		scenarioMark = rd.markAborted
		rd.ranScenarios += 1
		rd.abortedScenarios += 1
	case event.Success:
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

	ranStagesScenario := 0
	sentStagesScenario := 0
	successfulStagesScenario := 0
	abortedStagesScenario := 0
	for _, stage := range result.StageResults {
		rd.totalStages += 1
		switch stage.Status {
		case event.Success:
			successfulStagesScenario += 1
			fallthrough
		case event.Failure:
			fallthrough
		case event.Error:
			ranStagesScenario += 1
			sentStagesScenario += stage.Attempts
		case event.Aborted:
			abortedStagesScenario += 1
			ranStagesScenario += 1
			sentStagesScenario += stage.Attempts
		default:
		}
	}

	rd.ranStages += sentStagesScenario
	rd.successfulStages += successfulStagesScenario
	rd.abortedStages += abortedStagesScenario
	skippedStagesScenario := len(result.StageResults) - ranStagesScenario
	failedStagesScenario := ranStagesScenario - successfulStagesScenario - abortedStagesScenario

	switch rd.cfg.Show {
	case config.None:
		return nil
	case config.NoSkip:
		if result.Status == event.Unset || result.Status == event.Skipped {
			return nil
		}
	case config.Failed:
		if result.Status != event.Failure && result.Status != event.Error {
			return nil
		}
	case config.All:
	}

	fullScenarioName := bytes.NewBufferString(result.Name)
	if len(result.Labels) > 0 {
		fullScenarioName.Write([]byte{' ', '('})
		fullScenarioName.WriteString(result.Labels[0])
		for _, label := range result.Labels[1:] {
			fullScenarioName.Write([]byte{',', ' '})
			fullScenarioName.WriteString(label)
		}
		fullScenarioName.WriteByte(')')
	}
	if result.TotalRunCount > 1 {
		numberString := strconv.Itoa(result.CntId + 1)
		fullScenarioName.Write([]byte{' ', '#'})
		fullScenarioName.WriteString(numberString)
	}

	rd.printed = true
	tally := rd.makeTally(successfulStagesScenario, failedStagesScenario, abortedStagesScenario, skippedStagesScenario)

	_, err := fmt.Fprintf(rd.writer, " %s %s (%s in %s)%s\n",
		scenarioMark,
		fullScenarioName,
		strings.Join(tally, " "),
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

	if !rd.cfg.Verbose && (result.Status == event.Success || result.Status == event.Unset || result.Status == event.Skipped) {
		return nil
	}

	for _, stage := range result.StageResults {
		err = rd.reportStage(stage)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rd *reportData) reportOutput(result aggregator.ScenarioResult) error {
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

func (rd *reportData) reportStage(stage aggregator.StageResult) error {
	stageMark := rd.markSuccess
	switch stage.Status {
	case event.Unset:
		fallthrough
	case event.Skipped:
		stageMark = rd.markSkipped
	case event.Aborted:
		stageMark = rd.markAborted
	case event.Success:
	default:
		stageMark = rd.markFail
	}

	stageTackOn := ""
	if rd.cfg.Verbose {
		stageTackOn = fmt.Sprintf(" [Started: %s]", stage.StartedAt.Format("15:04:05.000"))
	}

	_, err := fmt.Fprintf(rd.writer, "   %s %s (%s)%s\n", stageMark, stage.Name, fmtDuration(stage.Total), stageTackOn)
	if err != nil {
		return err
	}

	if len(stage.FailedAssertions) > 0 {
		_, err = fmt.Fprintf(rd.writer, "     %s Failed assertions: %s\n", rd.markFail, strings.Join(stage.FailedAssertions, ", "))

		if err != nil {
			return err
		}
	}
	if err == nil && stage.ErrorMessage != "" {
		_, err = fmt.Fprintf(rd.writer, "     %s Error: %s\n", rd.markFail, stage.ErrorMessage)
		if err != nil {
			return err
		}
	}

	if rd.cfg.Verbose {
		err = rd.reportDurations(stage)
		if err != nil {
			return err
		}
	}

	if stage.Request != "" {
		_, err = fmt.Fprint(rd.writer, "     Request:\n")
		if err != nil {
			return err
		}
		_, err = io.WriteString(rd.prefixedStageWriter, stage.Request)
		if err != nil {
			return err
		}
	}

	if stage.Response != "" {
		_, err = fmt.Fprint(rd.writer, "     Response:\n")
		if err != nil {
			return err
		}
		_, err = io.WriteString(rd.prefixedStageWriter, stage.Response)
		if err != nil {
			return err
		}
	}
	return nil
}

func (rd *reportData) reportDurations(stage aggregator.StageResult) error {
	_, err := reportTime(rd.prefixedVerboseWriter, "Pre:    %8s\n", stage.Pre)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Mat:    %8s\n", stage.Materialization)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Parse:  %8s\n", stage.Parsing)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Exec:   %8s\n", stage.Execution)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Retry:  %8s\n", stage.Retry)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Assert: %8s\n", stage.Assert)
	if err != nil {
		return err
	}
	_, err = reportTime(rd.prefixedVerboseWriter, "Post:   %8s\n", stage.Post)
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

	tally := rd.makeTally(rd.successfulScenarios, failedScenarios, abortedScenarios, skippedScenarios)
	prefix := ""
	if rd.printed {
		prefix = "\n"
	}

	plural := "s"
	if rd.ranStages == 1 {
		plural = ""
	}

	_, err := fmt.Fprintf(rd.writer, "%s %s %s in %s | Scenarios: %s | %d request%s sent\n",
		prefix,
		finalMark,
		statusText,
		fmtDuration(duration),
		strings.Join(tally, " "),
		rd.ranStages,
		plural,
	)
	return err
}

func (rd *reportData) makeTally(success, failed, aborted, skipped int) []string {
	res := make([]string, 0, 4)
	if success != 0 {
		res = append(res, strconv.Itoa(success)+rd.markSuccess)
	}
	if failed != 0 {
		res = append(res, strconv.Itoa(failed)+rd.markFail)
	}
	if aborted != 0 {
		res = append(res, strconv.Itoa(aborted)+rd.markAborted)
	}
	if skipped != 0 {
		res = append(res, strconv.Itoa(skipped)+rd.markSkipped)
	}
	return res
}
