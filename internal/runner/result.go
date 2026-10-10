// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"fmt"
	"time"
)

type ResultStatus int

const (
	Unset ResultStatus = iota
	Skipped
	Success
	Aborted
	Failure
	Error
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
	Attempts         int
}

func (rr *RequestResult) merge(other RequestResult) {
	rr.Status = max(rr.Status, other.Status)
	rr.FailedAssertions = append(rr.FailedAssertions, other.FailedAssertions...)
	if rr.ErrorMessage == "" {
		rr.ErrorMessage = other.ErrorMessage
	}
	if rr.Request == "" {
		rr.Request = other.Request
	}
	if rr.Response == "" {
		rr.Response = other.Response
	}
	if rr.StartedAt.IsZero() {
		rr.StartedAt = other.StartedAt
	}
	rr.Pre += other.Pre
	rr.Materialization += other.Materialization
	rr.Parsing += other.Parsing
	rr.Execution += other.Execution
	rr.Retry += other.Retry
	rr.Assert += other.Assert
	rr.Post += other.Post
	rr.Total += other.Total
	rr.Attempts += other.Attempts
}

type ScenarioResult struct {
	Name           string
	Tags           []string
	StartedAt      time.Time
	TotalDuration  time.Duration
	RequestResults []RequestResult
	Status         ResultStatus
	Output         *bytes.Buffer
}

func (s ResultStatus) String() string {
	switch s {
	case Unset:
		fallthrough
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
