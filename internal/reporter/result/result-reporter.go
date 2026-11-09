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
	status event.ResultStatus
}

func NewResultReporter() *ResultReporter {
	return &ResultReporter{event.Success}
}

func (r *ResultReporter) Report(source <-chan aggregator.ScenarioResult, timer <-chan time.Duration, size int) error {
	count := 0
	for result := range source {
		r.status = max(r.status, result.Status)
		count += 1
		for _, stageResult := range result.StageResults {
			r.status = max(r.status, stageResult.Status)
		}

	}
	<-timer
	if count != size {
		r.status = max(r.status, event.Aborted)
	}
	return nil
}

func (r *ResultReporter) Status() event.ResultStatus {
	return r.status
}
