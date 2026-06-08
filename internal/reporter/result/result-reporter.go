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
	for result := range source {
		if !r.success {
			continue
		}

		for _, reqResult := range result.RequestResults {
			if reqResult.Status == runner.Failure || reqResult.Status == runner.Error {
				r.success = false
				break
			}
		}

	}
	<-timer
	return nil
}

func (r *ResultReporter) Success() bool {
	return r.success
}
