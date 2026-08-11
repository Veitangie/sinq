// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"errors"
	"fmt"
	"io"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/reporter"
	"veitangie.dev/sinq/internal/runner"
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

func (r StandardReporter) Report(source <-chan runner.ScenarioResult, total <-chan time.Duration, size int) error {
	var err error
	rd := newReportData(r.cfg, r.writer, size)
	for result := range source {
		if err != nil {
			continue
		}

		err = rd.report(result)
	}

	err2 := rd.reportEnd(<-total)
	return errors.Join(err, err2)
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
