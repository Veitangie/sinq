package event

import (
	"bytes"
	"fmt"
	"log/slog"
	"time"
)

type EventType int

const (
	Log EventType = iota
	ScenarioStart
	RequestStart
	PreScript
	Materialization
	Parsing
	Execution
	RetryScript
	AssertScript
	PostScript
	RequestEnd
	ScenarioEnd
	FileOpened
	FileClosed
	WorkerError
	RunnerDone
)

const (
	CachedMask int64 = 1 << iota
	CacheHitMask
	OversizedMask
	SingleflightLeaderMask
	SkippedScenarioMask
	ErrorScenarioMask
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

type RunId struct {
	SeqId int
	MatId int
	CntId int
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

type SubSystem int

const (
	Sinq SubSystem = iota
	Treewalker
	Runner
	Reporter
	EventBus
)

func (s SubSystem) String() string {
	switch s {
	case Sinq:
		return "Sinq"
	case Treewalker:
		return "Treewalker"
	case Runner:
		return "Runner"
	case Reporter:
		return "Reporter"
	case EventBus:
		return "EventBus"
	}
	return "Unknown"
}

type Event struct {
	Error            error
	Tags             []string
	Labels           []string
	RequestNames     []string
	FailedAssertions []string
	Attrs            []slog.Attr
	Name             string
	Filename         string
	ErrorString      string
	Message          string
	Request          string
	Response         string
	Output           *bytes.Buffer
	Duration         time.Duration
	StartedAt        time.Time
	Flags            int64
	Kind             EventType
	Level            slog.Level
	RunId            RunId
	RequestIdx       int
	Size             int
	WorkerId         int
	WorkerSeq        int
	TotalRunCount    int
	Source           SubSystem
	Status           ResultStatus
}
