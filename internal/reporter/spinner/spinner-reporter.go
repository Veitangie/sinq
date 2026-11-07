// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package spinner

import (
	"errors"
	"sync/atomic"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/sinq/internal/ui"
	"veitangie.dev/spinq"
)

type SpinnerReporter struct {
	spinner spinq.Writer
	clock   timer.Clock
	willRun int
	color   bool
}

func NewSpinnerReporter(spinner spinq.Writer, clock timer.Clock, willRun int, color bool) (*SpinnerReporter, error) {
	if spinner == nil {
		return nil, errors.New("Unable to construct spinner reporter: spinner is nil")
	}
	if clock == nil {
		clock = timer.DefaultClock{}
	}

	return &SpinnerReporter{
		spinner: spinner,
		clock:   clock,
		willRun: willRun,
		color:   color,
	}, nil
}

func (r *SpinnerReporter) Report(source <-chan aggregator.ScenarioResult, duration <-chan time.Duration, _ int) error {
	var err error
	if r.willRun < 10 {
		err = r.handleSimple(source)
	} else {
		err = r.handleBar(source)
	}
	if err != nil && errors.Is(err, spinq.ErrClosed) {
		err = nil
	}

	stopErr := r.spinner.Stop()
	<-duration
	if stopErr != nil && errors.Is(stopErr, spinq.ErrClosed) {
		stopErr = nil
	}
	return errors.Join(err, stopErr)
}

func (r *SpinnerReporter) handleBar(source <-chan aggregator.ScenarioResult) error {
	ran := &atomic.Int64{}
	err := r.spinner.SetFrame(ui.GetBarFrame(r.color, r.willRun, func() (int, int) { return int(ran.Load()), r.willRun }, r.spinner.GetWidth()))
	for res := range source {
		if res.Status != event.Unset && res.Status != event.Skipped {
			ran.Add(1)
		}
	}
	return err
}

func (r *SpinnerReporter) handleSimple(source <-chan aggregator.ScenarioResult) error {
	err := r.spinner.SetFrame(ui.GetSimpleFrame(r.color, r.clock, "Running"))
	for range source {
	}
	return err
}
