// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/scenario"
)

func runSource(t *testing.T, raw string, setup ...func(*worker)) (aggregator.ScenarioResult, *observer) {
	t.Helper()
	return runSourceWith(t, raw, nil, setup...)
}

func runSourceWith(t *testing.T, raw string, configure func(*scenario.ScenarioConfig), setup ...func(*worker)) (aggregator.ScenarioResult, *observer) {
	t.Helper()
	return runSourceIn(t, raw, &mockWorkspace{}, configure, setup...)
}

func runSourceIn(t *testing.T, raw string, ws Workspace, configure func(*scenario.ScenarioConfig), setup ...func(*worker)) (aggregator.ScenarioResult, *observer) {
	t.Helper()
	w := setupTestWorker(t, nil)
	w.env.transport = &countingRoundTripper{}
	for _, fn := range setup {
		fn(w)
	}
	obs := observe(t, w)

	bundle := eventsBundle(t, raw, configure)
	bundle.workspace = ws
	w.processScenario(context.Background(), bundle)
	return obs.result(), obs
}

func eventsBundle(t *testing.T, raw string, configure func(*scenario.ScenarioConfig)) taskBundle {
	t.Helper()
	bps, err := scenario.ParseRequestBlueprints(strings.NewReader(raw), "events.sinq")
	if err != nil {
		t.Fatalf("Failed to parse requests: %v", err)
	}
	cfg := &scenario.ScenarioConfig{
		Name:          "Events",
		ReqTimeout:    scenario.Duration{Duration: time.Second},
		ScriptTimeout: scenario.Duration{Duration: time.Second},
		MaxBodySize:   scenario.SaneDefaultConfig().MaxBodySize,
		MaxRetries:    3,
	}
	if configure != nil {
		configure(cfg)
	}
	return taskBundle{
		Config:    cfg,
		Requests:  bps,
		workspace: &mockWorkspace{},
		run:       true,
	}
}

func viaTransport(rt http.RoundTripper) func(*worker) {
	return func(w *worker) { w.env.transport = rt }
}

func requestEndsFor(obs *observer, idx int) []event.Event {
	res := []event.Event{}
	for _, e := range obs.ofKind(event.RequestEnd) {
		if e.RequestIdx == idx {
			res = append(res, e)
		}
	}
	return res
}

func executions(obs *observer) []event.Event {
	return obs.ofKind(event.Execution)
}

func hasFlag(e event.Event, mask int64) bool {
	return e.Flags&mask != 0
}

func failFast(cfg *scenario.ScenarioConfig) { cfg.FailFast = true }

func TestWorker_ProcessScenario_UnnamedRequestUsesFilename(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.transport = mockRoundTripper{}
	obs := observe(t, w)

	w.processScenario(context.Background(), taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "Unnamed"},
		Requests:  []*scenario.RequestBlueprint{{Filename: "01_login.sinq"}},
		workspace: &mockWorkspace{},
	})

	if got := obs.result().RequestResults[0].Name; got != "01_login.sinq" {
		t.Errorf("Expected an unnamed request to be reported as its filename, got %q", got)
	}
}

func TestWorker_Send_WorkerSeqCountsUpWithoutGaps(t *testing.T) {
	_, obs := runSource(t, "GET http://localhost/a HTTP/1.1\n\n### Second\nGET http://localhost/b HTTP/1.1\n")

	evs := obs.events()
	if len(evs) < 5 {
		t.Fatalf("Expected a scenario to send a handful of events, got %d", len(evs))
	}
	for i, e := range evs {
		if e.WorkerSeq != i+1 {
			t.Fatalf("Event %d (%v): expected WorkerSeq %d, got %d", i, e.Kind, i+1, e.WorkerSeq)
		}
	}
}

func TestWorker_Send_NilReportContextFallsBack(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.reportCtx = nil
	obs := observe(t, w)

	w.send(w.log(context.Background(), slog.LevelDebug, "still delivered"))
	if !obs.logged("still delivered") {
		t.Errorf("Expected the event to be sent with a nil report context")
	}
}

func TestWorker_ProcessScenario_FailFastPassingRequestsContinue(t *testing.T) {
	transport := &countingRoundTripper{}
	res, _ := runSourceWith(t, "GET http://localhost/a HTTP/1.1\n\n$ASSERT{ sinq.assert.code(200) }\n\n### Second\nGET http://localhost/b HTTP/1.1\n",
		failFast, func(w *worker) { w.env.transport = transport })

	if res.Status != event.Success {
		t.Errorf("Expected passing requests under fail_fast to succeed, got %v", res.Status)
	}
	for idx, rr := range res.RequestResults {
		if rr.Status != event.Success {
			t.Errorf("Request %d: expected Success, got %v", idx, rr.Status)
		}
	}
	if n := transport.sent.Load(); n != 2 {
		t.Errorf("Expected both requests to be sent, got %d", n)
	}
}

func TestWorker_ProcessScenario_SkippedZeroRequestScenarioIsSkipped(t *testing.T) {
	w := setupTestWorker(t, nil)
	obs := observe(t, w)

	w.processScenario(context.Background(), taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "Empty"},
		Requests:  []*scenario.RequestBlueprint{},
		workspace: &mockWorkspace{},
		run:       false,
	})

	if got := obs.result().Status; got != event.Skipped && got != event.Unset {
		t.Errorf("Expected a scenario that wasn't selected to run to be Skipped, got %v", got)
	}
}

func TestWorker_ProcessScenario_ScenarioStartCarriesSkippedFlag(t *testing.T) {
	cases := []struct {
		name     string
		run      bool
		requests []*scenario.RequestBlueprint
		flagged  bool
	}{
		{"not selected, with requests", false, []*scenario.RequestBlueprint{{Filename: "a.sinq"}}, true},
		{"not selected, no requests", false, []*scenario.RequestBlueprint{}, true},
		{"selected, no requests", true, []*scenario.RequestBlueprint{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := setupTestWorker(t, nil)
			w.env.transport = mockRoundTripper{}
			obs := observe(t, w)

			w.processScenario(context.Background(), taskBundle{
				Config:    &scenario.ScenarioConfig{Name: "Flag"},
				Requests:  tc.requests,
				workspace: &mockWorkspace{},
				run:       tc.run,
			})

			starts := obs.ofKind(event.ScenarioStart)
			if len(starts) != 1 {
				t.Fatalf("Expected one ScenarioStart, got %d", len(starts))
			}
			if got := hasFlag(starts[0], event.SkippedScenarioMask); got != tc.flagged {
				t.Errorf("Expected SkippedScenarioMask %v, got flags %b", tc.flagged, starts[0].Flags)
			}
			if !tc.run && len(obs.ofKind(event.RequestStart)) != 0 {
				t.Errorf("Expected a skipped run to start no requests")
			}
		})
	}
}

func TestWorker_ProcessRequest_RequestStartCarriesFilename(t *testing.T) {
	_, obs := runSource(t, "GET http://localhost/ HTTP/1.1\n")

	starts := obs.ofKind(event.RequestStart)
	if len(starts) != 1 {
		t.Fatalf("Expected one RequestStart event, got %d", len(starts))
	}
	if starts[0].Filename != "events.sinq" {
		t.Errorf("Expected RequestStart's Filename to be the request's file, got %q", starts[0].Filename)
	}
}

func TestWorker_ProcessScenario_SkippedScenarioHasNoPreviousOutput(t *testing.T) {
	w := setupTestWorker(t, nil)
	w.env.cfg.Print = true
	w.env.transport = mockRoundTripper{}
	obs := observe(t, w)

	printing, err := scenario.ParseRequestBlueprints(strings.NewReader("$PRE{ print(\"from A\") }\nGET http://localhost/ HTTP/1.1\n"), "a.sinq")
	if err != nil {
		t.Fatalf("Failed to parse requests: %v", err)
	}
	w.processScenario(context.Background(), taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "A", ReqTimeout: scenario.Duration{Duration: time.Second}, ScriptTimeout: scenario.Duration{Duration: time.Second}},
		Requests:  printing,
		workspace: &mockWorkspace{},
		runId:     event.RunId{SeqId: 1},
		run:       true,
	})
	a := obs.result()
	if a.Output == nil || !strings.Contains(a.Output.String(), "from A") {
		t.Fatalf("Expected scenario A to capture its own print output, got %v", a.Output)
	}

	w.processScenario(context.Background(), taskBundle{
		Config:    &scenario.ScenarioConfig{Name: "B"},
		Requests:  []*scenario.RequestBlueprint{{Filename: "b.sinq"}},
		workspace: &mockWorkspace{},
		runId:     event.RunId{SeqId: 2},
		run:       false,
	})
	if b := obs.result(); b.Output != nil && strings.Contains(b.Output.String(), "from A") {
		t.Errorf("Expected skipped scenario B to have no output, got A's: %q", b.Output.String())
	}
}

func TestWorker_ProcessRequest_AssertAndPostRun(t *testing.T) {
	raw := "GET http://localhost/ HTTP/1.1\n\n$ASSERT{ sinq.assert.code(200); print(\"assert ran\") }\n$POST{ print(\"post ran\") }\n"
	res, _ := runSource(t, raw, func(w *worker) { w.env.cfg.Print = true })

	if rr := res.RequestResults[0]; rr.Status != event.Success {
		t.Fatalf("Expected the request to pass its assertion, got %v %v %s", rr.Status, rr.FailedAssertions, rr.ErrorMessage)
	}
	if res.Output == nil || res.Output.String() != "assert ran\npost ran\n" {
		t.Errorf("Expected $ASSERT and then $POST to run, got %v", res.Output)
	}
}

func TestWorker_ProcessScenario_EndsExactlyOnce(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"passing", "GET http://localhost/ HTTP/1.1\n"},
		{"failing assertion", "GET http://localhost/ HTTP/1.1\n\n$ASSERT{ sinq.assert.fail(\"boom\") }\n"},
		{"erroring script", "GET http://localhost/ HTTP/1.1\n\n$POST{ error(\"boom\") }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, obs := runSource(t, tc.raw)

			ends := obs.ofKind(event.ScenarioEnd)
			if len(ends) != 1 {
				t.Fatalf("Expected exactly one ScenarioEnd, got %d: %+v", len(ends), ends)
			}
			if hasFlag(ends[0], event.ErrorScenarioMask) {
				t.Errorf("Expected no ErrorScenarioMask on a run that didn't panic, got flags %b", ends[0].Flags)
			}
		})
	}
}

func TestWorker_ProcessScenario_LogsTellRunsOfOneScenarioApart(t *testing.T) {
	w := setupTestWorker(t, nil)
	obs := observe(t, w)

	runs := []struct {
		labels []string
		runId  event.RunId
	}{
		{labels: []string{"admin"}, runId: event.RunId{SeqId: 1, MatId: 0}},
		{labels: []string{"guest"}, runId: event.RunId{SeqId: 1, MatId: 1}},
		{labels: []string{"guest"}, runId: event.RunId{SeqId: 1, MatId: 1, CntId: 1}},
	}
	cfg := &scenario.ScenarioConfig{Name: "checkout"}
	for _, r := range runs {
		ctx := context.WithValue(context.Background(), scenarioNameContext, cfg.Name)
		w.processScenario(ctx, taskBundle{
			Config:    cfg,
			Requests:  []*scenario.RequestBlueprint{{Filename: "a.sinq"}},
			workspace: &mockWorkspace{},
			labels:    r.labels,
			runId:     r.runId,
			run:       false,
		})
		obs.result()
	}

	seen := map[string]bool{}
	for _, e := range obs.ofKind(event.Log) {
		if e.Message != "[Runner] Starting scenario" {
			continue
		}
		attrs := slog.GroupValue(e.Attrs...).String()
		if seen[attrs] {
			t.Errorf("Expected each run's log attributes to identify it, got the same %s for two runs", attrs)
		}
		seen[attrs] = true
	}
	if len(seen) == 0 {
		t.Fatal("Expected a \"[Runner] Starting scenario\" log for each run, got none")
	}
}
