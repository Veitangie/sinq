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
	reset := Reset
	red := Red
	green := Green
	yellow := Yellow

	if r.cfg.Color == config.Never {
		reset = ""
		red = ""
		green = ""
		yellow = ""
	}

	resetBytes := []byte(reset)
	total := 0
	passed := 0
	for result := range source {
		total += 1
		scenarioColor := green
		switch result.Status {
		case runner.Aborted:
			scenarioColor = yellow
		case runner.Success:
			passed += 1
		default:
			scenarioColor = red
		}

		_, err = fmt.Fprintf(r.writer, "%sScenario: %s, Started At: %s\n", scenarioColor, result.Name, result.StartedAt.Format(time.RFC3339))
		if err != nil {
			continue
		}
		_, err = fmt.Fprintf(r.writer, "Status: %v, Total duration: %v%s\n", result.Status, result.TotalDuration, reset)
		if err != nil {
			continue
		}

		for idx, request := range result.RequestResults {
			if err != nil {
				continue
			}
			requestColor := green
			switch request.Status {
			case runner.Aborted:
				requestColor = yellow
			case runner.Success:
			default:
				requestColor = red
			}

			_, err = fmt.Fprintf(r.writer, "%s - %0d: Request %s, Started At: %v, Status: %v\n", requestColor, idx, request.Name, request.StartedAt, request.Status)
			if err != nil {
				continue
			}
			_, err = fmt.Fprintf(r.writer, " - Duration total: %v\n", request.Total)
			if err != nil {
				continue
			}
			if r.cfg.Verbose {
				_, err = reportTime(r.writer, "   - Pre script duration: %v\n", request.Pre)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "   - Materialization duration: %v\n", request.Materialization)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "   - Parsing duration: %v\n", request.Parsing)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "   - Execution duration: %v\n", request.Execution)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "   - Retry script duration: %v\n", request.Retry)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "   - Assert script duration: %v\n", request.Assert)
				if err != nil {
					continue
				}
				_, err = reportTime(r.writer, "   - Post script duration: %v\n", request.Post)
				if err != nil {
					continue
				}
			}
			if err == nil && len(request.FailedAssertions) > 0 {
				_, err = fmt.Fprintf(r.writer, " - Failed assertions: %s\n", strings.Join(request.FailedAssertions, ","))

				if err != nil {
					continue
				}
			}
			if err == nil && request.ErrorMessage != "" {
				_, err = fmt.Fprintf(r.writer, " - Error: %s\n", request.ErrorMessage)
				if err != nil {
					continue
				}
			}

			_, err = r.writer.Write(resetBytes)
			if err != nil {
				continue
			}
			_, err = r.writer.Write([]byte{'\n'})
			if err != nil {
				continue
			}
		}
	}

	if err == nil {
		finalColor := green
		if passed != total {
			finalColor = red
		}
		_, err = fmt.Fprintf(r.writer, "%sTotal tests: %d, successful: %d, total duration: %v%s\n", finalColor, total, passed, <-timer, reset)
	}
	return err
}

func reportTime(writer io.Writer, format string, duration time.Duration) (int, error) {
	if duration > 0 {
		return fmt.Fprintf(writer, format, duration)
	}
	return 0, nil
}
