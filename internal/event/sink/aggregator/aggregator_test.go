// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package aggregator_test

import (
	"bytes"
	"hash/fnv"
	"os"
	"os/exec"
	"slices"
	"sync"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
)

const aggTestTimeout = 2 * time.Second

var t0 = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

func rid(name string) event.RunId {
	h := fnv.New32a()
	h.Write([]byte(name))
	return event.RunId{SeqId: int(h.Sum32())}
}

func done() event.Event {
	return event.Event{Kind: event.RunnerDone, Duration: 42 * time.Second}
}

func feed(events ...event.Event) <-chan event.Event {
	in := make(chan event.Event, len(events))
	for _, e := range events {
		in <- e
	}
	close(in)
	return in
}

func run(events ...event.Event) <-chan aggregator.ScenarioResult {
	out, _ := aggregator.NewAggregator().AggregateOver(feed(append(events, done())...))
	return out
}

func collect(t *testing.T, out <-chan aggregator.ScenarioResult, n int) []aggregator.ScenarioResult {
	t.Helper()
	got := make([]aggregator.ScenarioResult, 0, n)
	timeout := time.After(aggTestTimeout)
	for len(got) < n {
		select {
		case r, ok := <-out:
			if !ok {
				t.Fatalf("Output closed after %d of %d expected results", len(got), n)
			}
			got = append(got, r)
		case <-timeout:
			t.Fatalf("Timed out after %d of %d expected results", len(got), n)
		}
	}
	return got
}

func expectNoMore(t *testing.T, out <-chan aggregator.ScenarioResult) {
	t.Helper()
	select {
	case r, ok := <-out:
		if ok {
			t.Fatalf("Expected no more results, got %+v", r)
		}
	case <-time.After(aggTestTimeout):
		t.Fatalf("Output channel was not closed")
	}
}

func start(name string, requests ...string) event.Event {
	return event.Event{
		Kind:         event.ScenarioStart,
		RunId:        rid(name),
		Name:         "scenario " + name,
		Tags:         []string{"smoke", "api"},
		StartedAt:    t0,
		RequestNames: requests,
	}
}

func end(name string) event.Event {
	return event.Event{Kind: event.ScenarioEnd, RunId: rid(name), Duration: 10 * time.Second}
}

func requestStart(name string, idx int) event.Event {
	return event.Event{Kind: event.RequestStart, RunId: rid(name), RequestIdx: idx, StartedAt: t0}
}

func stage(name string, kind event.EventType, idx int, d time.Duration) event.Event {
	return event.Event{Kind: kind, RunId: rid(name), RequestIdx: idx, Duration: d}
}

func dumpedEnd(name string, idx int, status event.ResultStatus, request, response string) event.Event {
	e := requestEnd(name, idx, status, "")
	e.Request, e.Response = request, response
	return e
}

func requestEnd(name string, idx int, status event.ResultStatus, errMsg string, failed ...string) event.Event {
	return event.Event{
		Kind:             event.RequestEnd,
		RunId:            rid(name),
		RequestIdx:       idx,
		Status:           status,
		ErrorString:      errMsg,
		FailedAssertions: failed,
		Duration:         time.Second,
	}
}

func pass(name string, idx int, status event.ResultStatus, failed ...string) []event.Event {
	return []event.Event{requestStart(name, idx), requestEnd(name, idx, status, "", failed...)}
}

func TestAggregator_StartAndEndBuildScenario(t *testing.T) {
	output := bytes.NewBufferString("printed")
	e := end("r1")
	e.Output = output

	got := collect(t, run(start("r1", "01.sinq", "login"), e), 1)[0]

	if got.Name != "scenario r1" {
		t.Errorf("Expected name from ScenarioStart, got %q", got.Name)
	}
	if !slices.Equal(got.Tags, []string{"smoke", "api"}) {
		t.Errorf("Expected tags from ScenarioStart, got %v", got.Tags)
	}
	if !got.StartedAt.Equal(t0) {
		t.Errorf("Expected StartedAt from ScenarioStart, got %v", got.StartedAt)
	}
	if got.TotalDuration != 10*time.Second {
		t.Errorf("Expected TotalDuration from ScenarioEnd, got %v", got.TotalDuration)
	}
	if got.Output != output {
		t.Errorf("Expected Output from ScenarioEnd")
	}
	if len(got.RequestResults) != 2 || got.RequestResults[0].Name != "01.sinq" || got.RequestResults[1].Name != "login" {
		t.Errorf("Expected one named request result per RequestNames entry, got %+v", got.RequestResults)
	}
	for idx, rr := range got.RequestResults {
		if rr.Status != event.Unset {
			t.Errorf("Expected request %d that never ran to stay Unset, got %v", idx, rr.Status)
		}
	}
}

func TestAggregator_StageDurationsLandOnTheirRequest(t *testing.T) {
	kinds := []struct {
		kind event.EventType
		get  func(aggregator.RequestResult) time.Duration
	}{
		{event.PreScript, func(r aggregator.RequestResult) time.Duration { return r.Pre }},
		{event.Materialization, func(r aggregator.RequestResult) time.Duration { return r.Materialization }},
		{event.Parsing, func(r aggregator.RequestResult) time.Duration { return r.Parsing }},
		{event.Execution, func(r aggregator.RequestResult) time.Duration { return r.Execution }},
		{event.RetryScript, func(r aggregator.RequestResult) time.Duration { return r.Retry }},
		{event.AssertScript, func(r aggregator.RequestResult) time.Duration { return r.Assert }},
		{event.PostScript, func(r aggregator.RequestResult) time.Duration { return r.Post }},
	}

	events := []event.Event{start("r1", "a", "b")}
	for i, k := range kinds {
		events = append(events, stage("r1", k.kind, 1, time.Duration(i+1)*time.Millisecond))
	}
	events = append(events, end("r1"))

	got := collect(t, run(events...), 1)[0]
	for i, k := range kinds {
		if d := k.get(got.RequestResults[1]); d != time.Duration(i+1)*time.Millisecond {
			t.Errorf("Stage %d: expected %v on request 1, got %v", k.kind, time.Duration(i+1)*time.Millisecond, d)
		}
		if d := k.get(got.RequestResults[0]); d != 0 {
			t.Errorf("Stage %d: expected nothing on request 0, got %v", k.kind, d)
		}
	}
}

func TestAggregator_RepeatedPassesSumDurations(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		stage("r1", event.Execution, 0, 2*time.Millisecond),
		requestEnd("r1", 0, event.Success, ""),
		stage("r1", event.Execution, 0, 3*time.Millisecond),
		requestEnd("r1", 0, event.Success, ""),
		end("r1"),
	), 1)[0]

	rr := got.RequestResults[0]
	if rr.Execution != 5*time.Millisecond {
		t.Errorf("Expected Execution summed over passes (5ms), got %v", rr.Execution)
	}
	if rr.Total != 2*time.Second {
		t.Errorf("Expected Total summed over passes (2s), got %v", rr.Total)
	}
}

func TestAggregator_RequestStartKeepsFirstPass(t *testing.T) {
	first := requestStart("r1", 0)
	first.StartedAt = t0.Add(time.Second)
	second := requestStart("r1", 0)
	second.StartedAt = t0.Add(5 * time.Second)

	got := collect(t, run(start("r1", "a"), first, second, end("r1")), 1)[0]
	if !got.RequestResults[0].StartedAt.Equal(t0.Add(time.Second)) {
		t.Errorf("Expected StartedAt from the first pass, got %v", got.RequestResults[0].StartedAt)
	}
}

func TestAggregator_FailedAssertionsAccumulateOverPasses(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		requestEnd("r1", 0, event.Failure, "", "first"),
		requestEnd("r1", 0, event.Failure, "", "second", "third"),
		end("r1"),
	), 1)[0]

	if want := []string{"first", "second", "third"}; !slices.Equal(got.RequestResults[0].FailedAssertions, want) {
		t.Errorf("Expected %v, got %v", want, got.RequestResults[0].FailedAssertions)
	}
}

func TestAggregator_RequestStatusFoldsWithMax(t *testing.T) {
	cases := []struct {
		name   string
		passes []event.ResultStatus
		want   event.ResultStatus
	}{
		{"earlier failed pass fails the request", []event.ResultStatus{event.Failure, event.Success}, event.Failure},
		{"ran then skipped is success", []event.ResultStatus{event.Success, event.Skipped}, event.Success},
		{"skipped then ran is success", []event.ResultStatus{event.Skipped, event.Success}, event.Success},
		{"failed then skipped is failure", []event.ResultStatus{event.Failure, event.Skipped}, event.Failure},
		{"error after failure is error", []event.ResultStatus{event.Failure, event.Error}, event.Error},
		{"later aborted pass is aborted", []event.ResultStatus{event.Success, event.Aborted}, event.Aborted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := []event.Event{start("r1", "a")}
			for _, s := range tc.passes {
				events = append(events, requestEnd("r1", 0, s, ""))
			}
			events = append(events, end("r1"))

			got := collect(t, run(events...), 1)[0]
			if got.RequestResults[0].Status != tc.want {
				t.Errorf("Passes %v: expected %v, got %v", tc.passes, tc.want, got.RequestResults[0].Status)
			}
		})
	}
}

func skippedStart(name string, requests ...string) event.Event {
	e := start(name, requests...)
	e.Flags |= event.SkippedScenarioMask
	return e
}

func TestAggregator_SkippedScenarioFlag(t *testing.T) {
	cases := []struct {
		name  string
		start event.Event
		want  event.ResultStatus
	}{
		{"flagged with requests", skippedStart("r1", "a", "b"), event.Skipped},
		{"flagged with no requests", skippedStart("r1"), event.Skipped},
		{"not flagged with no requests", start("r1"), event.Success},
		{"not flagged, requests never started", start("r1", "a", "b"), event.Success},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collect(t, run(tc.start, end("r1")), 1)[0]
			if got.Status != tc.want {
				t.Errorf("Expected %v (%d), got %v (%d)", tc.want, tc.want, got.Status, got.Status)
			}
			for idx, rr := range got.RequestResults {
				if rr.Status != event.Unset {
					t.Errorf("Expected request %d of a run that didn't run to stay Unset, got %v", idx, rr.Status)
				}
			}
		})
	}
}

func errorEnd(name string) event.Event {
	e := end(name)
	e.Flags |= event.ErrorScenarioMask
	e.ErrorString = "panic: boom"
	return e
}

func TestAggregator_ErrorScenarioFlag(t *testing.T) {
	cases := []struct {
		name   string
		events []event.Event
		want   event.ResultStatus
	}{
		{"no request started", []event.Event{start("r1", "a"), errorEnd("r1")}, event.Error},
		{"after a successful request", []event.Event{
			start("r1", "a", "b"),
			requestStart("r1", 0),
			requestEnd("r1", 0, event.Success, ""),
			requestStart("r1", 1),
			errorEnd("r1"),
		}, event.Error},
		{"after a failed request", []event.Event{
			start("r1", "a"),
			requestStart("r1", 0),
			requestEnd("r1", 0, event.Failure, ""),
			errorEnd("r1"),
		}, event.Error},
		{"not flagged", []event.Event{start("r1", "a"), requestStart("r1", 0), requestEnd("r1", 0, event.Success, ""), end("r1")}, event.Success},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := collect(t, run(tc.events...), 1)[0]
			if got.Status != tc.want {
				t.Errorf("Expected %v (%d), got %v (%d)", tc.want, tc.want, got.Status, got.Status)
			}
		})
	}

	got := collect(t, run(
		start("r1", "a", "b"),
		requestStart("r1", 0),
		requestEnd("r1", 0, event.Success, ""),
		errorEnd("r1"),
	), 1)[0]
	if s := got.RequestResults[0].Status; s != event.Success {
		t.Errorf("Expected the finished request to keep its Success, got %v (%d)", s, s)
	}
	if s := got.RequestResults[1].Status; s != event.Unset {
		t.Errorf("Expected the unreached request to stay Unset, got %v (%d)", s, s)
	}
}

func TestAggregator_ErrorScenarioFlagIsPerRun(t *testing.T) {
	got := collect(t, run(
		start("broken", "a"),
		start("fine", "a"),
		requestStart("fine", 0),
		requestEnd("fine", 0, event.Success, ""),
		errorEnd("broken"),
		end("fine"),
	), 2)

	byName := map[string]event.ResultStatus{}
	for _, r := range got {
		byName[r.Name] = r.Status
	}
	if s := byName["scenario broken"]; s != event.Error {
		t.Errorf("Expected the flagged run to be Error, got %v (%d)", s, s)
	}
	if s := byName["scenario fine"]; s != event.Success {
		t.Errorf("Expected the other run to stay Success, got %v (%d)", s, s)
	}
}

func TestAggregator_SkippedScenarioFlagIsPerRun(t *testing.T) {
	got := collect(t, run(
		skippedStart("skipped", "a"),
		start("ran", "a"),
		requestStart("ran", 0),
		requestEnd("ran", 0, event.Success, ""),
		end("skipped"),
		end("ran"),
	), 2)

	byName := map[string]event.ResultStatus{}
	for _, r := range got {
		byName[r.Name] = r.Status
	}
	if s := byName["scenario skipped"]; s != event.Skipped {
		t.Errorf("Expected the flagged run to be Skipped, got %v (%d)", s, s)
	}
	if s := byName["scenario ran"]; s != event.Success {
		t.Errorf("Expected the run without the flag to be Success, got %v (%d)", s, s)
	}
}

func TestAggregator_SkippedScenarioLeftOpenIsAborted(t *testing.T) {
	got := collect(t, run(skippedStart("r1", "a")), 1)[0]
	if got.Status != event.Aborted {
		t.Errorf("Expected an open run to be Aborted on RunnerDone even if flagged skipped, got %v", got.Status)
	}
}

func TestAggregator_ScenarioStatusIsNeverUnset(t *testing.T) {
	runs := map[string][]event.Event{
		"skipped":                {skippedStart("r", "a")},
		"no requests":            {start("r")},
		"requests never started": {start("r", "a", "b")},
		"skipped request":        append([]event.Event{start("r", "a")}, pass("r", 0, event.Skipped)...),
		"successful request":     append([]event.Event{start("r", "a")}, pass("r", 0, event.Success)...),
		"failed request":         append([]event.Event{start("r", "a")}, pass("r", 0, event.Failure)...),
		"errored request":        append([]event.Event{start("r", "a")}, pass("r", 0, event.Error)...),
		"aborted request":        append([]event.Event{start("r", "a")}, pass("r", 0, event.Aborted)...),
		"end without a start":    {requestEnd("r", 0, event.Success, "")},
	}
	exits := map[string]func([]event.Event) <-chan aggregator.ScenarioResult{
		"ScenarioEnd": func(evs []event.Event) <-chan aggregator.ScenarioResult {
			return run(append(evs, end("r"))...)
		},
		"RunnerDone": func(evs []event.Event) <-chan aggregator.ScenarioResult {
			return run(evs...)
		},
		"input closed": func(evs []event.Event) <-chan aggregator.ScenarioResult {
			out, _ := aggregator.NewAggregator().AggregateOver(feed(evs...))
			return out
		},
	}

	for runName, evs := range runs {
		for exitName, exit := range exits {
			t.Run(runName+" via "+exitName, func(t *testing.T) {
				out := exit(evs)
				for {
					select {
					case r, ok := <-out:
						if !ok {
							return
						}
						if r.Status == event.Unset {
							t.Errorf("Expected a ScenarioResult never to come out Unset, got it for %q", r.Name)
						}
					case <-time.After(aggTestTimeout):
						t.Fatal("Timed out waiting for the output to close")
					}
				}
			})
		}
	}
}

func TestAggregator_UnreachedRequestStaysUnset(t *testing.T) {
	events := append([]event.Event{start("r1", "a", "b")}, pass("r1", 0, event.Failure)...)
	got := collect(t, run(append(events, end("r1"))...), 1)[0]
	if got.Status == event.Unset {
		t.Fatalf("Expected the scenario to have a status, got Unset")
	}
	if s := got.RequestResults[1].Status; s != event.Unset {
		t.Errorf("Expected the request that was never reached to stay Unset, got %v (%d)", s, s)
	}
}

func TestAggregator_ScenarioStatusFromRequests(t *testing.T) {
	cases := []struct {
		name     string
		statuses []event.ResultStatus
		want     event.ResultStatus
	}{
		{"all requests succeed", []event.ResultStatus{event.Success, event.Success}, event.Success},
		{"an assertion failure is a Failure, not an Error", []event.ResultStatus{event.Success, event.Failure}, event.Failure},
		{"a later passing request doesn't undo a failure", []event.ResultStatus{event.Failure, event.Success}, event.Failure},
		{"an error is an Error", []event.ResultStatus{event.Success, event.Error}, event.Error},
		{"an error after a failure is an Error", []event.ResultStatus{event.Failure, event.Error}, event.Error},
		{"a cancelled request aborts the scenario", []event.ResultStatus{event.Success, event.Aborted}, event.Aborted},
		{"a skipped request still counts as ran", []event.ResultStatus{event.Skipped, event.Success}, event.Success},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := []event.Event{start("r1", "a", "b")}
			for idx, s := range tc.statuses {
				events = append(events, pass("r1", idx, s)...)
			}
			events = append(events, end("r1"))

			got := collect(t, run(events...), 1)[0]
			if got.Status != tc.want {
				t.Errorf("Requests %v: expected scenario %v, got %v", tc.statuses, tc.want, got.Status)
			}
		})
	}
}

func TestAggregator_ErrorMessageKeepsFirstNonEmpty(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		requestEnd("r1", 0, event.Error, "first error"),
		requestEnd("r1", 0, event.Success, ""),
		requestEnd("r1", 0, event.Error, "second error"),
		end("r1"),
	), 1)[0]

	if got.RequestResults[0].ErrorMessage != "first error" {
		t.Errorf("Expected the first non-empty error message, got %q", got.RequestResults[0].ErrorMessage)
	}
}

func TestAggregator_InterleavedRunsStaySeparate(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		start("r2", "a"),
		stage("r2", event.Execution, 0, 7*time.Millisecond),
		stage("r1", event.Execution, 0, 1*time.Millisecond),
		requestEnd("r1", 0, event.Failure, "", "r1 failed"),
		requestEnd("r2", 0, event.Success, ""),
		end("r2"),
		end("r1"),
	), 2)

	byName := map[string]aggregator.ScenarioResult{}
	for _, r := range got {
		byName[r.Name] = r
	}
	r1, r2 := byName["scenario r1"], byName["scenario r2"]
	if r1.RequestResults[0].Execution != time.Millisecond || r2.RequestResults[0].Execution != 7*time.Millisecond {
		t.Errorf("Expected per-run durations 1ms/7ms, got %v/%v", r1.RequestResults[0].Execution, r2.RequestResults[0].Execution)
	}
	if !slices.Equal(r1.RequestResults[0].FailedAssertions, []string{"r1 failed"}) || len(r2.RequestResults[0].FailedAssertions) != 0 {
		t.Errorf("Expected the assertion failure only on r1, got r1=%v r2=%v", r1.RequestResults[0].FailedAssertions, r2.RequestResults[0].FailedAssertions)
	}
}

func TestAggregator_RunsDifferingOnlyInMatrixOrCountStaySeparate(t *testing.T) {
	ids := []event.RunId{{SeqId: 1, MatId: 0, CntId: 0}, {SeqId: 1, MatId: 1, CntId: 0}, {SeqId: 1, MatId: 0, CntId: 1}}
	events := []event.Event{}
	for i, id := range ids {
		events = append(events, event.Event{Kind: event.ScenarioStart, RunId: id, Name: "same", RequestNames: []string{"a"}})
		events = append(events, event.Event{Kind: event.Execution, RunId: id, Duration: time.Duration(i+1) * time.Millisecond})
	}
	for _, id := range ids {
		events = append(events, event.Event{Kind: event.ScenarioEnd, RunId: id})
	}

	got := collect(t, run(events...), len(ids))
	durations := []time.Duration{}
	for _, r := range got {
		durations = append(durations, r.RequestResults[0].Execution)
	}
	slices.Sort(durations)
	if want := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond}; !slices.Equal(durations, want) {
		t.Errorf("Expected each run to keep its own Execution, got %v", durations)
	}
}

func TestAggregator_EventsForUnknownRunsAreIgnored(t *testing.T) {
	out := run(
		stage("ghost", event.Execution, 0, time.Millisecond),
		end("ghost"),
		start("r1", "a"),
		end("r1"),
		stage("r1", event.Execution, 0, time.Millisecond),
		end("r1"),
	)

	got := collect(t, out, 1)
	if got[0].Name != "scenario r1" {
		t.Errorf("Expected only r1, got %q", got[0].Name)
	}
	expectNoMore(t, out)
}

func TestAggregator_RunnerDoneClosesOutputAndReportsDuration(t *testing.T) {
	out, durations := aggregator.NewAggregator().AggregateOver(feed(start("r1", "a"), end("r1"), done()))
	collect(t, out, 1)
	expectNoMore(t, out)

	select {
	case d := <-durations:
		if d != 42*time.Second {
			t.Errorf("Expected the RunnerDone duration (42s), got %v", d)
		}
	case <-time.After(aggTestTimeout):
		t.Fatal("Timed out waiting for the total duration")
	}
}

func TestAggregator_OutputClosesWhenInputEndsWithoutRunnerDone(t *testing.T) {
	out, _ := aggregator.NewAggregator().AggregateOver(feed(start("r1", "a"), end("r1")))
	collect(t, out, 1)
	expectNoMore(t, out)
}

func TestAggregator_RunnerDoneEndsStreamEvenWithZeroRunIdOpen(t *testing.T) {
	in := make(chan event.Event, 2)
	defer close(in)
	out, _ := aggregator.NewAggregator().AggregateOver(in)

	in <- event.Event{Kind: event.ScenarioStart, RunId: event.RunId{}, Name: "zero", RequestNames: []string{"a"}}
	in <- done()

	got := collect(t, out, 1)[0]
	if got.Status != event.Aborted {
		t.Errorf("Expected the open run to be Aborted on RunnerDone, got %v", got.Status)
	}
	expectNoMore(t, out)
}

func TestAggregator_UnfinishedRunIsAbortedOnRunnerDone(t *testing.T) {
	got := collect(t, run(start("r1", "a", "b"), requestEnd("r1", 0, event.Success, "")), 1)[0]
	if got.Status != event.Aborted {
		t.Errorf("Expected an unfinished run to be Aborted, got %v", got.Status)
	}
	if got.RequestResults[0].Status != event.Success {
		t.Errorf("Expected the finished request to keep its status, got %v", got.RequestResults[0].Status)
	}
}

func TestAggregator_AttemptsCountExecutions(t *testing.T) {
	got := collect(t, run(
		start("r1", "a", "b"),
		stage("r1", event.Execution, 0, time.Millisecond),
		stage("r1", event.Execution, 0, time.Millisecond),
		requestEnd("r1", 0, event.Success, ""),
		stage("r1", event.Execution, 0, time.Millisecond),
		requestEnd("r1", 0, event.Success, ""),
		end("r1"),
	), 1)[0]

	if got.RequestResults[0].Attempts != 3 {
		t.Errorf("Expected 3 attempts over both passes, got %d", got.RequestResults[0].Attempts)
	}
	if got.RequestResults[1].Attempts != 0 {
		t.Errorf("Expected no attempts on a request that never ran, got %d", got.RequestResults[1].Attempts)
	}
}

func TestAggregator_SuccessfulRequestHasNoDump(t *testing.T) {
	for _, status := range []event.ResultStatus{event.Success, event.Skipped} {
		got := collect(t, run(
			start("r1", "a"),
			dumpedEnd("r1", 0, status, "GET /ok", "200 ok"),
			end("r1"),
		), 1)[0]

		if rr := got.RequestResults[0]; rr.Request != "" || rr.Response != "" {
			t.Errorf("%v: expected no dump for a request that didn't fail, got %q / %q", status, rr.Request, rr.Response)
		}
	}
}

func TestAggregator_FailedPassKeepsDump(t *testing.T) {
	for _, status := range []event.ResultStatus{event.Failure, event.Error, event.Aborted} {
		got := collect(t, run(
			start("r1", "a"),
			dumpedEnd("r1", 0, status, "GET /x", "500 x"),
			end("r1"),
		), 1)[0]

		if rr := got.RequestResults[0]; rr.Request != "GET /x\n" || rr.Response != "500 x\n" {
			t.Errorf("%v: expected the pass's dump, got %q / %q", status, rr.Request, rr.Response)
		}
	}
}

func TestAggregator_DumpIsFromFailingPass(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		dumpedEnd("r1", 0, event.Success, "GET /clean", "200 clean"),
		dumpedEnd("r1", 0, event.Failure, "GET /failing", "500 failing"),
		end("r1"),
	), 1)[0]

	if rr := got.RequestResults[0]; rr.Request != "GET /failing\n" || rr.Response != "500 failing\n" {
		t.Errorf("Expected the dump from the failing pass, got %q / %q", rr.Request, rr.Response)
	}
}

func TestAggregator_FirstFailingPassKeepsItsDump(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		dumpedEnd("r1", 0, event.Failure, "GET /first", "500 first"),
		dumpedEnd("r1", 0, event.Error, "GET /second", "500 second"),
		end("r1"),
	), 1)[0]

	if rr := got.RequestResults[0]; rr.Request != "GET /first\n" || rr.Response != "500 first\n" {
		t.Errorf("Expected the first failing pass's dump, got %q / %q", rr.Request, rr.Response)
	}
}

func TestAggregator_LaterCleanPassKeepsFailingDump(t *testing.T) {
	got := collect(t, run(
		start("r1", "a"),
		dumpedEnd("r1", 0, event.Failure, "GET /failing", "500 failing"),
		dumpedEnd("r1", 0, event.Success, "GET /clean", "200 clean"),
		end("r1"),
	), 1)[0]

	rr := got.RequestResults[0]
	if rr.Status != event.Failure {
		t.Fatalf("Expected the request to stay Failure, got %v", rr.Status)
	}
	if rr.Request != "GET /failing\n" || rr.Response != "500 failing\n" {
		t.Errorf("Expected the failing pass's dump to survive the later clean pass, got %q / %q", rr.Request, rr.Response)
	}
}

func TestAggregator_DumpsOnExecutionAreIgnored(t *testing.T) {
	ex := stage("r1", event.Execution, 0, time.Millisecond)
	ex.Request, ex.Response = "GET /from-execution", "200 from execution"

	got := collect(t, run(
		start("r1", "a"),
		ex,
		requestEnd("r1", 0, event.Failure, "", "boom"),
		end("r1"),
	), 1)[0]

	if rr := got.RequestResults[0]; rr.Request != "" || rr.Response != "" {
		t.Errorf("Expected only RequestEnd to supply dumps, got %q / %q", rr.Request, rr.Response)
	}
}

func TestAggregator_LabelsComeFromScenarioStart(t *testing.T) {
	s := start("r1", "a")
	s.Labels = []string{"staging", "eu"}

	got := collect(t, run(s, end("r1")), 1)[0]
	if !slices.Equal(got.Labels, []string{"staging", "eu"}) {
		t.Errorf("Expected labels from ScenarioStart, got %v", got.Labels)
	}
}

func checkOutOfRangeIgnored(t *testing.T, idx int) {
	t.Helper()
	got := collect(t, run(
		start("r1", "a"),
		requestStart("r1", idx),
		stage("r1", event.Execution, idx, time.Millisecond),
		requestEnd("r1", idx, event.Failure, "bad index"),
		end("r1"),
	), 1)[0]

	if rr := got.RequestResults[0]; rr.Execution != 0 || rr.ErrorMessage != "" {
		t.Errorf("RequestIdx %d: expected the event to be ignored, request 0 got %+v", idx, rr)
	}
}

func TestAggregator_OutOfRangeRequestIdxIsIgnored(t *testing.T) {
	for _, idx := range []int{1, 5} {
		checkOutOfRangeIgnored(t, idx)
	}
}

func TestAggregator_NegativeRequestIdxIsIgnored(t *testing.T) {
	if os.Getenv("SINQ_AGG_NEGATIVE_IDX_CHILD") == "1" {
		checkOutOfRangeIgnored(t, -1)
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestAggregator_NegativeRequestIdxIsIgnored$")
	cmd.Env = append(os.Environ(), "SINQ_AGG_NEGATIVE_IDX_CHILD=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("Expected a negative RequestIdx to be ignored, the aggregator crashed: %v\n%s", err, out)
	}
}

func TestAggregator_ScenarioEndIgnoresRequestIdx(t *testing.T) {
	cases := []struct {
		name  string
		start event.Event
		idx   int
	}{
		{"zero requests", start("r1"), 0},
		{"index past the last request", start("r1", "a"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := end("r1")
			e.RequestIdx = tc.idx

			got := collect(t, run(tc.start, e), 1)[0]
			if got.Status == event.Aborted {
				t.Errorf("Expected the run to be finished by its ScenarioEnd, it was aborted on RunnerDone")
			}
		})
	}
}

func TestAggregator_SecondCallWhileRunningGetsClosedChannel(t *testing.T) {
	agg := aggregator.NewAggregator()
	in := make(chan event.Event)
	first, _ := agg.AggregateOver(in)

	second, secondDurations := agg.AggregateOver(feed(start("other", "a"), end("other"), done()))
	expectNoMore(t, second)
	if _, ok := <-secondDurations; ok {
		t.Error("Expected the second call's duration channel to be closed")
	}

	in <- start("r1", "a")
	in <- end("r1")
	if got := collect(t, first, 1)[0]; got.Name != "scenario r1" {
		t.Errorf("Expected the running aggregation to be unaffected, got %q", got.Name)
	}
	in <- done()
	expectNoMore(t, first)
	close(in)
}

func TestAggregator_ConcurrentCallsOnlyOneAggregates(t *testing.T) {
	const callers = 32
	agg := aggregator.NewAggregator()
	release := make(chan struct{})
	inputs := make([]chan event.Event, callers)
	outputs := make([]<-chan aggregator.ScenarioResult, callers)

	var wg sync.WaitGroup
	for i := range callers {
		inputs[i] = make(chan event.Event, 3)
		wg.Go(func() {
			<-release
			outputs[i], _ = agg.AggregateOver(inputs[i])
		})
	}
	close(release)
	wg.Wait()

	live := -1
	for i, out := range outputs {
		select {
		case r, ok := <-out:
			if ok {
				t.Fatalf("Caller %d got a result before any input: %+v", i, r)
			}
		case <-time.After(50 * time.Millisecond):
			if live != -1 {
				t.Fatalf("Expected exactly one running aggregation, callers %d and %d both got live channels", live, i)
			}
			live = i
		}
	}
	if live == -1 {
		t.Fatalf("Expected one caller to get a running aggregation, all %d got closed channels", callers)
	}

	inputs[live] <- start("r1", "a")
	inputs[live] <- end("r1")
	inputs[live] <- done()
	close(inputs[live])
	collect(t, outputs[live], 1)
	expectNoMore(t, outputs[live])
	for i, in := range inputs {
		if i != live {
			close(in)
		}
	}
}

func TestAggregator_KeepsDrainingAfterRunnerDone(t *testing.T) {
	in := make(chan event.Event)
	out, _ := aggregator.NewAggregator().AggregateOver(in)

	sent := make(chan struct{})
	go func() {
		in <- start("r1", "a")
		in <- end("r1")
		in <- done()
		for range 100 {
			in <- start("late", "a")
			in <- end("late")
		}
		close(sent)
	}()

	collect(t, out, 1)
	expectNoMore(t, out)
	select {
	case <-sent:
	case <-time.After(aggTestTimeout):
		t.Fatal("The aggregator stopped reading its input after RunnerDone")
	}
	close(in)
}

func TestAggregator_SeparateAggregatorsRunInParallel(t *testing.T) {
	const n = 16
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			events := []event.Event{start("r1", "a")}
			for range i {
				events = append(events, stage("r1", event.Execution, 0, time.Millisecond))
			}
			events = append(events, end("r1"))

			out := run(events...)
			got := collect(t, out, 1)[0]
			if got.RequestResults[0].Attempts != i {
				t.Errorf("Aggregator %d: expected %d attempts, got %d", i, i, got.RequestResults[0].Attempts)
			}
			expectNoMore(t, out)
		})
	}
	wg.Wait()
}
