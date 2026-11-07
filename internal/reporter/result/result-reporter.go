// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package result

import (
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
)

type ResultReporter struct {
	success bool
}

func NewResultReporter() *ResultReporter {
	return &ResultReporter{true}
}

func (r *ResultReporter) Report(source <-chan aggregator.ScenarioResult, timer <-chan time.Duration, size int) error {
	count := 0
	for result := range source {
		if !r.success {
			continue
		}

		if result.Status == event.Failure || result.Status == event.Error || result.Status == event.Aborted {
			r.success = false
			continue
		}

		count += 1
		for _, reqResult := range result.RequestResults {
			if reqResult.Status == event.Failure || reqResult.Status == event.Error || reqResult.Status == event.Aborted {
				r.success = false
				break
			}
		}

	}
	<-timer
	r.success = r.success && count == size
	return nil
}

func (r *ResultReporter) Success() bool {
	return r.success
}
