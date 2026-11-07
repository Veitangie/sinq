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

type RequestResult struct {
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
	Status           event.ResultStatus
	Attempts         int
}

type ScenarioResult struct {
	Tags           []string
	Labels         []string
	RequestResults []RequestResult
	Name           string
	Output         *bytes.Buffer
	StartedAt      time.Time
	TotalDuration  time.Duration
	Status         event.ResultStatus
	CntId          int
	TotalRunCount  int
}

type enrichedScenarioResult struct {
	Tags           []string
	Labels         []string
	RequestResults []enrichedRequestResult
	Name           string
	Output         *bytes.Buffer
	StartedAt      time.Time
	TotalDuration  time.Duration
	Status         event.ResultStatus
	CntId          int
	TotalRunCount  int
}

func (e enrichedScenarioResult) output() ScenarioResult {
	res := ScenarioResult{
		Tags:           e.Tags,
		Labels:         e.Labels,
		RequestResults: make([]RequestResult, 0, len(e.RequestResults)),
		Name:           e.Name,
		Output:         e.Output,
		StartedAt:      e.StartedAt,
		TotalDuration:  e.TotalDuration,
		Status:         e.Status,
		CntId:          e.CntId,
		TotalRunCount:  e.TotalRunCount,
	}

	for _, er := range e.RequestResults {
		res.RequestResults = append(res.RequestResults, er.RequestResult)
	}

	return res
}

type enrichedRequestResult struct {
	RequestResult
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
				res.RequestResults = make([]enrichedRequestResult, len(e.RequestNames))
				for idx := range res.RequestResults {
					res.RequestResults[idx].Name = e.RequestNames[idx]
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

			eventIsRequest := e.Kind >= event.RequestStart && e.Kind <= event.RequestEnd
			if eventIsRequest && (e.RequestIdx >= len(res.RequestResults) || e.RequestIdx < 0) {
				continue
			}

			a.handleEvent(resCh, res, e)
		}
	}()

	return resCh, durationCh
}

func (a *Aggregator) handleEvent(resCh chan<- ScenarioResult, res *enrichedScenarioResult, e event.Event) {
	switch e.Kind {
	case event.RequestStart:
		if res.Status == event.Unset {
			res.Status = event.Success
		}
		if res.RequestResults[e.RequestIdx].StartedAt.IsZero() {
			res.RequestResults[e.RequestIdx].StartedAt = e.StartedAt
		}
	case event.PreScript:
		res.RequestResults[e.RequestIdx].Pre += e.Duration
	case event.Materialization:
		res.RequestResults[e.RequestIdx].Materialization += e.Duration
	case event.Parsing:
		res.RequestResults[e.RequestIdx].Parsing += e.Duration
	case event.Execution:
		res.RequestResults[e.RequestIdx].Execution += e.Duration
		res.RequestResults[e.RequestIdx].Attempts += 1
	case event.RetryScript:
		res.RequestResults[e.RequestIdx].Retry += e.Duration
	case event.AssertScript:
		res.RequestResults[e.RequestIdx].Assert += e.Duration
	case event.PostScript:
		res.RequestResults[e.RequestIdx].Post += e.Duration
	case event.RequestEnd:
		res.RequestResults[e.RequestIdx].Total += e.Duration
		switch e.Status {
		case event.Failure:
			fallthrough
		case event.Error:
			fallthrough
		case event.Aborted:
			res.Status = max(res.Status, e.Status)
			if !res.RequestResults[e.RequestIdx].keepDumps {
				res.RequestResults[e.RequestIdx].Request = withNewline(e.Request)
				res.RequestResults[e.RequestIdx].Response = withNewline(e.Response)
				res.RequestResults[e.RequestIdx].keepDumps = true
			}
		default:
			if !res.RequestResults[e.RequestIdx].keepDumps {
				res.RequestResults[e.RequestIdx].Request = ""
				res.RequestResults[e.RequestIdx].Response = ""
			}
		}

		if len(e.FailedAssertions) != 0 {
			res.RequestResults[e.RequestIdx].FailedAssertions = append(res.RequestResults[e.RequestIdx].FailedAssertions, e.FailedAssertions...)
		}
		if res.RequestResults[e.RequestIdx].ErrorMessage == "" {
			res.RequestResults[e.RequestIdx].ErrorMessage = e.ErrorString
		}

		res.RequestResults[e.RequestIdx].Status = max(res.RequestResults[e.RequestIdx].Status, e.Status)
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
