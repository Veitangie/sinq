// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package result

import (
	"time"

	"github.com/Veitangie/sinq/internal/runner"
)

type ResultReporter struct {
	success bool
}

func NewResultReporter() *ResultReporter {
	return &ResultReporter{true}
}

func (r *ResultReporter) Report(source <-chan runner.ScenarioResult, timer <-chan time.Duration, size int) error {
	count := 0
	for result := range source {
		if !r.success {
			continue
		}

		if result.Status == runner.Failure || result.Status == runner.Error || result.Status == runner.Aborted {
			r.success = false
			continue
		}

		count += 1
		for _, reqResult := range result.RequestResults {
			if reqResult.Status == runner.Failure || reqResult.Status == runner.Error || reqResult.Status == runner.Aborted {
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
