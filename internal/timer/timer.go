// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package timer

import "time"

type Clock interface {
	Now() time.Time
}

type DefaultClock struct{}

func (d DefaultClock) Now() time.Time {
	return time.Now()
}

type Timer struct {
	clock       Clock
	at          time.Time
	initialized bool
}

func NewTimer(clock Clock) Timer {
	if clock == nil {
		clock = DefaultClock{}
	}
	return Timer{
		at:          clock.Now(),
		clock:       clock,
		initialized: true,
	}
}

func (r *Timer) Start() {
	r.at = r.clock.Now()
	r.initialized = true
}

func (r *Timer) Time() time.Duration {
	if !r.initialized {
		return time.Duration(0)
	}
	return r.clock.Now().Sub(r.at)
}

func (r *Timer) StartedAt() time.Time {
	return r.at
}
