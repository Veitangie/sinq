// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"fmt"
	"time"
)

type Clock interface {
	Now() time.Time
}

type DefaultClock struct{}

func (d DefaultClock) Now() time.Time {
	return time.Now()
}

type ResultStatus int

const (
	Aborted ResultStatus = iota
	Success
	Error
	Failure
)

type ResultTimer struct {
	clock       Clock
	at          time.Time
	initialized bool
}

func newTimer(clock Clock) ResultTimer {
	if clock == nil {
		clock = DefaultClock{}
	}
	return ResultTimer{
		at:          clock.Now(),
		clock:       clock,
		initialized: true,
	}
}

func (r *ResultTimer) start() {
	r.at = r.clock.Now()
	r.initialized = true
}

func (r *ResultTimer) Time() time.Duration {
	if !r.initialized {
		return time.Duration(0)
	}
	return r.clock.Now().Sub(r.at)
}

type RequestResult struct {
	Status           ResultStatus
	FailedAssertions []string
	Name             string
	ErrorMessage     string
	StartedAt        time.Time
	Pre              time.Duration
	Materialization  time.Duration
	Parsing          time.Duration
	Execution        time.Duration
	Retry            time.Duration
	Assert           time.Duration
	Post             time.Duration
	Total            time.Duration
}

type ScenarioResult struct {
	Name           string
	StartedAt      time.Time
	TotalDuration  time.Duration
	RequestResults []RequestResult
	Status         ResultStatus
}

func (s ResultStatus) String() string {
	switch s {
	case Aborted:
		return "Aborted/Skipped"
	case Success:
		return "Success"
	case Failure:
		return "Failure"
	case Error:
		return "Error"
	default:
		return fmt.Sprintf("Unknown(%d)", int(s))
	}
}
