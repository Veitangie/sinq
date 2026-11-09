package aggregator

import (
	"bytes"
	"sync"
	"time"

	"veitangie.dev/sinq/internal/event"
)

type Aggregator struct {
	guard     sync.Mutex
	running   bool
	completed bool
	store     map[event.RunId]*enrichedScenarioResult
}

func NewAggregator() *Aggregator {
	return &Aggregator{
		guard: sync.Mutex{},
		store: map[event.RunId]*enrichedScenarioResult{},
	}
}

type StageResult struct {
	FailedAssertions []string
	Name             string
	Filename         string
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
	Status           event.ResultStatus
	Attempts         int
}

type ScenarioResult struct {
	Tags          []string
	Labels        []string
	StageResults  []StageResult
	Name          string
	Output        *bytes.Buffer
	StartedAt     time.Time
	TotalDuration time.Duration
	Status        event.ResultStatus
	CntId         int
	TotalRunCount int
}

type enrichedScenarioResult struct {
	Tags          []string
	Labels        []string
	StageResults  []enrichedStageResult
	Name          string
	Output        *bytes.Buffer
	StartedAt     time.Time
	TotalDuration time.Duration
	Status        event.ResultStatus
	CntId         int
	TotalRunCount int
}

func (e enrichedScenarioResult) output() ScenarioResult {
	res := ScenarioResult{
		Tags:          e.Tags,
		Labels:        e.Labels,
		StageResults:  make([]StageResult, 0, len(e.StageResults)),
		Name:          e.Name,
		Output:        e.Output,
		StartedAt:     e.StartedAt,
		TotalDuration: e.TotalDuration,
		Status:        e.Status,
		CntId:         e.CntId,
		TotalRunCount: e.TotalRunCount,
	}

	for _, er := range e.StageResults {
		res.StageResults = append(res.StageResults, er.StageResult)
	}

	return res
}

type enrichedStageResult struct {
	StageResult
	keepDumps bool
}

func (a *Aggregator) AggregateOver(es <-chan event.Event) (<-chan ScenarioResult, <-chan time.Duration) {
	resCh := make(chan ScenarioResult, 1)
	durationCh := make(chan time.Duration, 1)

	a.guard.Lock()
	defer a.guard.Unlock()

	if a.running {
		close(resCh)
		close(durationCh)
		return resCh, durationCh
	}
	a.running = true
	a.completed = false

	go func() {
		defer func() {
			for runId, res := range a.store {
				res.Status = event.Aborted
				resCh <- res.output()
				delete(a.store, runId)
			}

			if !a.completed {
				close(resCh)
				close(durationCh)
			}

			a.guard.Lock()
			a.running = false
			a.guard.Unlock()
		}()

		for e := range es {
			if a.completed {
				continue
			}

			switch e.Kind {
			case event.ScenarioStart:
				res := &enrichedScenarioResult{}
				a.store[e.RunId] = res

				res.Name = e.Name
				res.Tags = e.Tags
				res.Labels = e.Labels
				res.StartedAt = e.StartedAt
				res.CntId = e.RunId.CntId
				res.TotalRunCount = e.TotalRunCount

				if e.Flags&event.SkippedScenarioMask != 0 {
					res.Status = event.Skipped
				}
				res.StageResults = make([]enrichedStageResult, len(e.StageNames))
				for idx := range res.StageResults {
					res.StageResults[idx].Name = e.StageNames[idx].Name
					res.StageResults[idx].Filename = e.StageNames[idx].Filename
				}
				continue
			case event.RunnerDone:
				for runId, res := range a.store {
					res.Status = event.Aborted
					resCh <- res.output()
					delete(a.store, runId)
				}
				close(resCh)
				durationCh <- e.Duration
				close(durationCh)
				a.completed = true
				continue
			default:
			}

			res, ok := a.store[e.RunId]
			if !ok {
				continue
			}

			eventIsStage := e.Kind >= event.StageStart && e.Kind <= event.StageEnd
			if eventIsStage && (e.StageIdx >= len(res.StageResults) || e.StageIdx < 0) {
				continue
			}

			a.handleEvent(resCh, res, e)
		}
	}()

	return resCh, durationCh
}

func (a *Aggregator) handleEvent(resCh chan<- ScenarioResult, res *enrichedScenarioResult, e event.Event) {
	switch e.Kind {
	case event.StageStart:
		if res.Status == event.Unset {
			res.Status = event.Success
		}
		if res.StageResults[e.StageIdx].StartedAt.IsZero() {
			res.StageResults[e.StageIdx].StartedAt = e.StartedAt
		}
	case event.PreScript:
		res.StageResults[e.StageIdx].Pre += e.Duration
	case event.Materialization:
		res.StageResults[e.StageIdx].Materialization += e.Duration
	case event.Parsing:
		res.StageResults[e.StageIdx].Parsing += e.Duration
	case event.Execution:
		res.StageResults[e.StageIdx].Execution += e.Duration
		res.StageResults[e.StageIdx].Attempts += 1
	case event.RetryScript:
		res.StageResults[e.StageIdx].Retry += e.Duration
	case event.AssertScript:
		res.StageResults[e.StageIdx].Assert += e.Duration
	case event.PostScript:
		res.StageResults[e.StageIdx].Post += e.Duration
	case event.StageEnd:
		res.StageResults[e.StageIdx].Total += e.Duration
		switch e.Status {
		case event.Failure:
			fallthrough
		case event.Error:
			fallthrough
		case event.Aborted:
			res.Status = max(res.Status, e.Status)
			if !res.StageResults[e.StageIdx].keepDumps {
				res.StageResults[e.StageIdx].Request = withNewline(e.Request)
				res.StageResults[e.StageIdx].Response = withNewline(e.Response)
				res.StageResults[e.StageIdx].keepDumps = true
			}
		default:
			if !res.StageResults[e.StageIdx].keepDumps {
				res.StageResults[e.StageIdx].Request = ""
				res.StageResults[e.StageIdx].Response = ""
			}
		}

		if len(e.FailedAssertions) != 0 {
			res.StageResults[e.StageIdx].FailedAssertions = append(res.StageResults[e.StageIdx].FailedAssertions, e.FailedAssertions...)
		}
		if res.StageResults[e.StageIdx].ErrorMessage == "" {
			res.StageResults[e.StageIdx].ErrorMessage = e.ErrorString
		}

		res.StageResults[e.StageIdx].Status = max(res.StageResults[e.StageIdx].Status, e.Status)
	case event.ScenarioEnd:
		delete(a.store, e.RunId)

		res.TotalDuration = e.Duration
		res.Output = e.Output
		if res.Status == event.Unset {
			res.Status = event.Success
		}
		if e.Flags&event.ErrorScenarioMask != 0 {
			res.Status = max(res.Status, event.Error)
		}

		resCh <- res.output()
	default:
	}
}

func withNewline(source string) string {
	if len(source) != 0 && source[len(source)-1] != '\n' {
		buf := bytes.NewBuffer(make([]byte, 0, len(source)+1))
		buf.WriteString(source)
		buf.WriteByte('\n')
		return buf.String()
	}
	return source
}
