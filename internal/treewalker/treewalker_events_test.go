// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package treewalker_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/treewalker"
)

func recordingBus(t *testing.T) (*event.Bus, func() []event.Event) {
	t.Helper()
	bus, err := event.NewBus(64, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Failed to construct event bus: %v", err)
	}
	sink := bus.RegisterMC(1024)
	got := make(chan []event.Event, 1)
	go func() {
		evs := []event.Event{}
		for e := range sink {
			evs = append(evs, e)
		}
		got <- evs
	}()
	bus.Start()

	return bus, func() []event.Event {
		bus.Close()
		select {
		case evs := <-got:
			return evs
		case <-time.After(2 * time.Second):
			t.Fatal("Timed out waiting for the bus to deliver")
			return nil
		}
	}
}

func attr(e event.Event, key string) (slog.Value, bool) {
	for _, a := range e.Attrs {
		if a.Key == key {
			return a.Value, true
		}
	}
	return slog.Value{}, false
}

func logsWith(evs []event.Event, msg string) []event.Event {
	res := []event.Event{}
	for _, e := range evs {
		if e.Kind == event.Log && strings.Contains(e.Message, msg) {
			res = append(res, e)
		}
	}
	return res
}

var eventsFS = fstest.MapFS{
	"root/shared.sinq":   {Data: []byte("SHARED")},
	"root/A/a.sinq":      {Data: []byte("A")},
	"root/B/b.sinq":      {Data: []byte("B")},
	"root/C/c.sinq":      {Data: []byte("C")},
	"root/C/c.scenario":  {Data: []byte(`{"name": "C"}`)},
	"root/D/d.sinq":      {Data: []byte("D")},
	"root/D/d2.sinq":     {Data: []byte("D2")},
	"root/D/e/deep.sinq": {Data: []byte("DEEP")},
}

func walkEvents(t *testing.T, workers int) ([]scenario.ScenarioBlueprint, []event.Event) {
	t.Helper()
	bus, events := recordingBus(t)
	walker, err := treewalker.NewTreewalker(config.Config{Workers: workers}, bus, mockParseRequest, mockParseConfig)
	if err != nil {
		t.Fatalf("Failed to create walker: %v", err)
	}
	ctx := context.WithValue(context.Background(), treewalker.PathCtxKey, "root")
	bps, err := walker.ParseFiletree(ctx, eventsFS, "root")
	if err != nil {
		t.Fatalf("ParseFiletree failed: %v", err)
	}
	return bps, events()
}

func TestTreewalkerEvents_SourceAndNoRun(t *testing.T) {
	_, evs := walkEvents(t, 4)
	if len(evs) == 0 {
		t.Fatal("Expected the walk to send events")
	}
	for _, e := range evs {
		if e.Source != event.Treewalker {
			t.Errorf("Expected Source Treewalker, got %v for %q", e.Source, e.Message)
		}
		if e.RunId != (event.RunId{}) {
			t.Errorf("Expected treewalker events to carry no RunId, got %+v for %q", e.RunId, e.Message)
		}
	}
}

func TestTreewalkerEvents_WorkerSeqPerSender(t *testing.T) {
	_, evs := walkEvents(t, 4)

	next := map[int]int{}
	for _, e := range evs {
		next[e.WorkerId]++
		if e.WorkerSeq != next[e.WorkerId] {
			t.Errorf("WorkerId %d: expected WorkerSeq %d, got %d for %q", e.WorkerId, next[e.WorkerId], e.WorkerSeq, e.Message)
		}
	}

	finished := logsWith(evs, "Finished discovery")
	if len(finished) != 1 || finished[0].WorkerId != 0 {
		t.Errorf("Expected one discovery total from the coordinator (WorkerId 0), got %+v", finished)
	}
	for _, e := range logsWith(evs, "Worker got task") {
		if e.WorkerId < 1 || e.WorkerId > 4 {
			t.Errorf("Expected worker events from WorkerId 1..4, got %d", e.WorkerId)
		}
	}
}

func TestTreewalkerEvents_WorkerLogsCarryWorkerAndSeqId(t *testing.T) {
	bps, evs := walkEvents(t, 2)

	seqIds := map[int64]bool{}
	for _, bp := range bps {
		seqIds[int64(bp.SeqId)] = true
	}
	tasks := logsWith(evs, "Worker got task")
	if len(tasks) != len(bps) {
		t.Fatalf("Expected one 'Worker got task' per scenario (%d), got %d", len(bps), len(tasks))
	}
	for _, e := range tasks {
		if v, ok := attr(e, "workerId"); !ok || v.Int64() != int64(e.WorkerId) {
			t.Errorf("Expected a workerId attr matching WorkerId %d, got %v", e.WorkerId, v)
		}
		if v, ok := attr(e, "seqId"); !ok || !seqIds[v.Int64()] {
			t.Errorf("Expected a seqId attr naming one of the scenarios, got %v", v)
		}
	}
}

func TestTreewalkerEvents_DiscoveryTotal(t *testing.T) {
	_, evs := walkEvents(t, 2)

	finished := logsWith(evs, "Finished discovery")
	if len(finished) != 1 {
		t.Fatalf("Expected one discovery total, got %d", len(finished))
	}
	if v, ok := attr(finished[0], "path"); !ok || v.String() != "root" {
		t.Errorf("Expected the walked root as the path attr, got %v", v)
	}
	if v, ok := attr(finished[0], "duration"); !ok || v.Duration() <= 0 {
		t.Errorf("Expected a positive duration attr, got %v", v)
	}
}

func TestTreewalkerEvents_ParseErrorIsLogged(t *testing.T) {
	bus, events := recordingBus(t)
	failing := func(r io.Reader, filename string) ([]*scenario.RequestBlueprint, error) {
		return nil, errors.New("unparseable")
	}
	walker, _ := treewalker.NewTreewalker(config.Config{Workers: 1}, bus, failing, mockParseConfig)
	if _, err := walker.ParseFiletree(context.Background(), fstest.MapFS{"root/A/a.sinq": {Data: []byte("A")}}, "root"); err == nil {
		t.Fatal("Expected the parse error to be returned")
	}

	logged := logsWith(events(), "Error occurred while parsing file")
	if len(logged) != 1 {
		t.Fatalf("Expected one parse error log, got %d", len(logged))
	}
	e := logged[0]
	if e.Level != slog.LevelError {
		t.Errorf("Expected Error level, got %v", e.Level)
	}
	if v, ok := attr(e, "filePath"); !ok || v.String() != "root/A/a.sinq" {
		t.Errorf("Expected the failing file as filePath, got %v", v)
	}
	if v, ok := attr(e, "error"); !ok || !strings.Contains(v.String(), "unparseable") {
		t.Errorf("Expected the parse error as the error attr, got %v", v)
	}
}

func TestTreewalkerEvents_SingleFileDurationCoversTheParse(t *testing.T) {
	bus, events := recordingBus(t)
	slow := func(r io.Reader, filename string) ([]*scenario.RequestBlueprint, error) {
		time.Sleep(20 * time.Millisecond)
		return mockParseRequest(r, filename)
	}
	walker, _ := treewalker.NewTreewalker(config.Config{Workers: 1}, bus, slow, mockParseConfig)
	if _, err := walker.ParseSingleFile(fstest.MapFS{"single.sinq": {Data: []byte("X")}}, "single.sinq"); err != nil {
		t.Fatalf("ParseSingleFile failed: %v", err)
	}

	finished := logsWith(events(), "Finished discovery")
	if len(finished) != 1 {
		t.Fatalf("Expected one discovery total, got %d", len(finished))
	}
	if v, ok := attr(finished[0], "duration"); !ok || v.Duration() < 20*time.Millisecond {
		t.Errorf("Expected the duration to cover the 20ms parse, got %v", v)
	}
}
