// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package reporter

import (
	"errors"
	"sync"
	"time"

	"github.com/Veitangie/sinq/internal/runner"
)

type Reporter interface {
	Report(<-chan runner.ScenarioResult, <-chan time.Duration, int) error
}

type ReporterPool struct {
	reporters []Reporter
	lock      sync.RWMutex
}

var _ Reporter = &ReporterPool{}

func NewPool(reps ...Reporter) *ReporterPool {
	return &ReporterPool{
		reporters: reps,
	}
}

func (r *ReporterPool) Register(rep Reporter) error {
	if rep == nil {
		return errors.New("Can't register a nil reporter")
	}
	r.lock.Lock()
	r.reporters = append(r.reporters, rep)
	r.lock.Unlock()
	return nil
}

func (r *ReporterPool) Report(source <-chan runner.ScenarioResult, timer <-chan time.Duration, size int) error {
	r.lock.RLock()
	sourceChans := make([]chan runner.ScenarioResult, 0, len(r.reporters))
	timerChans := make([]chan time.Duration, 0, len(r.reporters))
	errorCh := make(chan error, len(r.reporters))
	wg := sync.WaitGroup{}

	for idx := range r.reporters {
		sCh := make(chan runner.ScenarioResult, size)
		tCh := make(chan time.Duration, 1)

		sourceChans = append(sourceChans, sCh)
		timerChans = append(timerChans, tCh)

		wg.Add(1)
		go func(rep Reporter, src <-chan runner.ScenarioResult, tmr <-chan time.Duration) {
			defer wg.Done()
			err := rep.Report(src, tmr, size)
			if err != nil {
				errorCh <- err
			}
		}(r.reporters[idx], sCh, tCh)
	}
	r.lock.RUnlock()

	go func() {
		for result := range source {
			for _, next := range sourceChans {
				next <- result
			}
		}

		for _, next := range sourceChans {
			close(next)
		}
		duration := <-timer
		for _, next := range timerChans {
			next <- duration
			close(next)
		}

	}()

	var err error
	wg.Wait()
	close(errorCh)
	for next := range errorCh {
		err = errors.Join(err, next)
	}
	return err
}
