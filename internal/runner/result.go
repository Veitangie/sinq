// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"fmt"
	"time"
)

type ResultStatus int

const (
	Skipped ResultStatus = iota
	Aborted
	Success
	Error
	Failure
)

type RequestResult struct {
	Status           ResultStatus
	FailedAssertions []string
	Name             string
	ErrorMessage     string
	Request          string
	Response         string
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
	Tags           []string
	StartedAt      time.Time
	TotalDuration  time.Duration
	RequestResults []RequestResult
	Status         ResultStatus
}

func (s ResultStatus) String() string {
	switch s {
	case Skipped:
		return "Skipped"
	case Aborted:
		return "Aborted"
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
