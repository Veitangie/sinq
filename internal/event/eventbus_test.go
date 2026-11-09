// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package event_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event"
)

const busTestTimeout = 2 * time.Second

func newTestBus(t *testing.T, bufsize int) *event.Bus {
	t.Helper()
	bus, err := event.NewBus(bufsize, slog.Default())
	if err != nil {
		t.Fatalf("NewBus failed: %v", err)
	}
	return bus
}

func drain(t *testing.T, ch <-chan event.Event) <-chan []event.Event {
	t.Helper()
	out := make(chan []event.Event, 1)
	go func() {
		got := make([]event.Event, 0)
		timeout := time.After(busTestTimeout)
		for {
			select {
			case e, ok := <-ch:
				if !ok {
					out <- got
					return
				}
				got = append(got, e)
			case <-timeout:
				out <- nil
				return
			}
		}
	}()
	return out
}

func await[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(busTestTimeout + time.Second):
		t.Fatalf("Timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func received(t *testing.T, ch <-chan []event.Event, what string) []event.Event {
	t.Helper()
	got := await(t, ch, what)
	if got == nil {
		t.Fatalf("%s: the sink channel was never closed", what)
	}
	return got
}

func closeWithin(t *testing.T, bus *event.Bus) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		bus.Close()
		close(done)
	}()
	await(t, done, "Close to return")
}

func sendWithin(t *testing.T, bus *event.Bus, events []event.Event) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for _, e := range events {
			bus.Send(context.Background(), e)
		}
		close(done)
	}()
	await(t, done, "Send calls to return")
}

func numbered(workerId, n int) []event.Event {
	events := make([]event.Event, n)
	for i := range events {
		events[i] = event.Event{Kind: event.Log, WorkerId: workerId, WorkerSeq: i + 1}
	}
	return events
}

func TestNewBus_NilLogger(t *testing.T) {
	if _, err := event.NewBus(1, nil); err == nil {
		t.Error("Expected an error for a nil logger")
	}
}

func TestEventBus_MCReceivesEverythingInOrder(t *testing.T) {
	bus := newTestBus(t, 1)
	mc := bus.RegisterMC(1)
	bus.Start()
	got := drain(t, mc)

	const n = 1000
	sendWithin(t, bus, numbered(1, n))
	closeWithin(t, bus)

	events := received(t, got, "MC sink")
	if len(events) != n {
		t.Fatalf("Expected the MC sink to receive all %d events, got %d", n, len(events))
	}
	for i, e := range events {
		if e.WorkerSeq != i+1 {
			t.Fatalf("Expected events in send order: position %d has WorkerSeq %d", i, e.WorkerSeq)
		}
	}
}

func TestEventBus_SlowMCIsWaitedFor(t *testing.T) {
	bus := newTestBus(t, 1)
	mc := bus.RegisterMC(1)
	bus.Start()

	got := make(chan int, 1)
	go func() {
		count := 0
		for range mc {
			time.Sleep(100 * time.Microsecond)
			count++
		}
		got <- count
	}()

	const n = 200
	sendWithin(t, bus, numbered(1, n))
	closeWithin(t, bus)

	if count := await(t, got, "slow MC sink to finish"); count != n {
		t.Errorf("Expected a slow MC sink to still receive all %d events, got %d", n, count)
	}
}

func TestEventBus_EveryMCGetsEveryEvent(t *testing.T) {
	bus := newTestBus(t, 1)
	first, second := bus.RegisterMC(1), bus.RegisterMC(4)
	bus.Start()
	gotFirst, gotSecond := drain(t, first), drain(t, second)

	const n = 300
	sendWithin(t, bus, numbered(1, n))
	closeWithin(t, bus)

	if events := received(t, gotFirst, "first MC sink"); len(events) != n {
		t.Errorf("Expected the first MC sink to get %d events, got %d", n, len(events))
	}
	if events := received(t, gotSecond, "second MC sink"); len(events) != n {
		t.Errorf("Expected the second MC sink to get %d events, got %d", n, len(events))
	}
}

func TestEventBus_ConcurrentSendersKeepPerWorkerOrder(t *testing.T) {
	bus := newTestBus(t, 8)
	mc := bus.RegisterMC(1)
	bus.Start()
	got := drain(t, mc)

	const workers, perWorker = 16, 200
	wg := sync.WaitGroup{}
	for w := range workers {
		wg.Go(func() {
			for _, e := range numbered(w, perWorker) {
				bus.Send(context.Background(), e)
			}
		})
	}
	sent := make(chan struct{})
	go func() { wg.Wait(); close(sent) }()
	await(t, sent, "concurrent senders")
	closeWithin(t, bus)

	events := received(t, got, "MC sink")
	if len(events) != workers*perWorker {
		t.Fatalf("Expected %d events, got %d", workers*perWorker, len(events))
	}
	last := make(map[int]int, workers)
	for _, e := range events {
		if e.WorkerSeq != last[e.WorkerId]+1 {
			t.Fatalf("Worker %d: expected WorkerSeq %d after %d, got %d", e.WorkerId, last[e.WorkerId]+1, last[e.WorkerId], e.WorkerSeq)
		}
		last[e.WorkerId] = e.WorkerSeq
	}
}

func TestEventBus_StalledBEDoesNotBlock(t *testing.T) {
	bus := newTestBus(t, 1)
	mc := bus.RegisterMC(1)
	be := bus.RegisterBE(1)
	bus.Start()
	gotMC := drain(t, mc)

	const n = 1000
	sendWithin(t, bus, numbered(1, n))
	gotBE := drain(t, be)
	closeWithin(t, bus)

	if events := received(t, gotMC, "MC sink"); len(events) != n {
		t.Errorf("Expected the MC sink to get all %d events despite a stalled BE sink, got %d", n, len(events))
	}
	beEvents := received(t, gotBE, "BE sink")
	if len(beEvents) >= n {
		t.Errorf("Expected a stalled BE sink with a buffer of 1 to miss events, got all %d", len(beEvents))
	}
	for i := 1; i < len(beEvents); i++ {
		if beEvents[i].WorkerSeq <= beEvents[i-1].WorkerSeq {
			t.Errorf("Expected the events a BE sink does get to stay in order, got WorkerSeq %d after %d", beEvents[i].WorkerSeq, beEvents[i-1].WorkerSeq)
		}
	}
}

func TestEventBus_KeptUpBEGetsEverything(t *testing.T) {
	bus := newTestBus(t, 1)
	be := bus.RegisterBE(1000)
	bus.Start()
	got := drain(t, be)

	const n = 500
	sendWithin(t, bus, numbered(1, n))
	closeWithin(t, bus)

	if events := received(t, got, "BE sink"); len(events) != n {
		t.Errorf("Expected a BE sink with room to spare to get all %d events, got %d", n, len(events))
	}
}

func TestEventBus_LogEventsReachSinks(t *testing.T) {
	bus := newTestBus(t, 1)
	mc := bus.RegisterMC(1)
	be := bus.RegisterBE(10)
	bus.Start()
	gotMC, gotBE := drain(t, mc), drain(t, be)

	sendWithin(t, bus, []event.Event{{Kind: event.Log, Source: event.Runner, Output: bytes.NewBufferString("hello")}})
	closeWithin(t, bus)

	for name, ch := range map[string]<-chan []event.Event{"MC": gotMC, "BE": gotBE} {
		events := received(t, ch, name+" sink")
		if len(events) != 1 || events[0].Kind != event.Log || events[0].Output.String() != "hello" {
			t.Errorf("Expected the %s sink to get the log event unchanged, got %+v", name, events)
		}
	}
}

func TestEventBus_CloseClosesEverySink(t *testing.T) {
	bus := newTestBus(t, 1)
	mc := bus.RegisterMC(1)
	be := bus.RegisterBE(1)
	bus.Start()
	gotMC, gotBE := drain(t, mc), drain(t, be)

	closeWithin(t, bus)

	received(t, gotMC, "MC sink")
	received(t, gotBE, "BE sink")
}

func TestEventBus_CloseWaitsForDelivery(t *testing.T) {
	bus := newTestBus(t, 64)
	mc := bus.RegisterMC(1)
	bus.Start()

	var count int
	var mu sync.Mutex
	consumed := make(chan struct{})
	go func() {
		for range mc {
			mu.Lock()
			count++
			mu.Unlock()
		}
		close(consumed)
	}()

	const n = 64
	sendWithin(t, bus, numbered(1, n))
	closeWithin(t, bus)

	mu.Lock()
	early := count
	mu.Unlock()
	if early < n-2 {
		t.Errorf("Expected Close to wait until the MC sink was handed all %d events, only %d were read when it returned", n, early)
	}

	await(t, consumed, "MC sink to be closed")
	if count != n {
		t.Errorf("Expected %d events in total, got %d", n, count)
	}
}

func TestEventBus_OnlyLogEventsReachTheLogger(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bus, err := event.NewBus(4, logger)
	if err != nil {
		t.Fatalf("NewBus failed: %v", err)
	}
	bus.Start()

	sendWithin(t, bus, []event.Event{
		{Kind: event.Log, Level: slog.LevelWarn, Message: "hello from a log event"},
		{Kind: event.Execution, Level: slog.LevelWarn, Message: "not a log event"},
	})
	closeWithin(t, bus)

	mu.Lock()
	defer mu.Unlock()
	out := buf.String()
	if !strings.Contains(out, "hello from a log event") {
		t.Errorf("Expected the Log event to be written to the logger, got %q", out)
	}
	if strings.Contains(out, "not a log event") || strings.Count(out, "\n") != 1 {
		t.Errorf("Expected only the Log event to be written to the logger, got %q", out)
	}
}

func TestEventBus_BEDropLogNamesTheSink(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := event.NewBus(4, logger)
	if err != nil {
		t.Fatalf("NewBus failed: %v", err)
	}
	bus.RegisterBE(0)
	bus.Start()

	sendWithin(t, bus, numbered(1, 1))
	closeWithin(t, bus)

	mu.Lock()
	defer mu.Unlock()
	var dropLine string
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if strings.Contains(line, "Failed to send event to BE sink") {
			dropLine = line
		}
	}
	if !strings.Contains(dropLine, "sinkId=1 ") {
		t.Errorf("Expected the drop to be logged for sinkId=1, got %q in %q", dropLine, buf.String())
	}
}

type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestEventBus_SendGivesUpWhenContextIsDone(t *testing.T) {
	bus := newTestBus(t, 1)
	sendWithin(t, bus, numbered(1, 1))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		bus.Send(ctx, numbered(1, 3)...)
		close(done)
	}()
	await(t, done, "Send with a done context on a full bus to return")
}

func TestEventBus_SendAfterCloseIsDropped(t *testing.T) {
	bus := newTestBus(t, 1)
	mc := bus.RegisterMC(1)
	bus.Start()
	got := drain(t, mc)

	sendWithin(t, bus, numbered(1, 3))
	closeWithin(t, bus)

	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		bus.Send(context.Background(), event.Event{Kind: event.FileClosed, Filename: "late.bin"})
	}()
	if panicked := await(t, done, "Send after Close"); panicked != nil {
		t.Errorf("Expected Send after Close to be dropped, it panicked: %v", panicked)
	}

	if events := received(t, got, "MC sink"); len(events) != 3 {
		t.Errorf("Expected only the 3 events sent before Close, got %d", len(events))
	}
}

func TestEventBus_SinkRegisteredMidRun(t *testing.T) {
	bus := newTestBus(t, 1)
	early := bus.RegisterMC(1)
	bus.Start()
	gotEarly := drain(t, early)

	sendWithin(t, bus, numbered(1, 10))

	lateMC := bus.RegisterMC(1)
	lateBE := bus.RegisterBE(100)
	gotLateMC, gotLateBE := drain(t, lateMC), drain(t, lateBE)

	sendWithin(t, bus, numbered(2, 20))
	closeWithin(t, bus)

	if events := received(t, gotEarly, "early MC sink"); len(events) != 30 {
		t.Errorf("Expected the early sink to get all 30 events, got %d", len(events))
	}
	for name, ch := range map[string]<-chan []event.Event{"late MC": gotLateMC, "late BE": gotLateBE} {
		events := received(t, ch, name+" sink")
		after := 0
		for _, e := range events {
			if e.WorkerId == 2 {
				after++
			}
		}
		if after != 20 {
			t.Errorf("Expected the %s sink to get all 20 events sent after it registered, got %d", name, after)
		}
	}
}

func TestEventBus_SendRacingCloseDoesNotPanic(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	var panics atomic.Int64
	var firstPanic atomic.Value

	for range 5000 {
		bus, err := event.NewBus(1, quiet)
		if err != nil {
			t.Fatalf("NewBus failed: %v", err)
		}
		mc := bus.RegisterMC(1)
		bus.Start()
		consumed := make(chan struct{})
		go func() {
			for range mc {
			}
			close(consumed)
		}()

		senders := sync.WaitGroup{}
		for range 8 {
			senders.Go(func() {
				defer func() {
					if r := recover(); r != nil {
						panics.Add(1)
						firstPanic.CompareAndSwap(nil, r)
					}
				}()
				for range 50 {
					bus.Send(context.Background(), event.Event{Kind: event.Log})
				}
			})
		}
		closeWithin(t, bus)
		senders.Wait()
		<-consumed
	}

	if n := panics.Load(); n > 0 {
		t.Errorf("Expected Send racing Close to be dropped, %d Send calls panicked, first with: %v", n, firstPanic.Load())
	}
}

func TestEventBus_RegisterAfterCloseReturnsClosedChannel(t *testing.T) {
	bus := newTestBus(t, 1)
	bus.Start()
	closeWithin(t, bus)

	received(t, drain(t, bus.RegisterMC(1)), "MC sink registered after Close")
	received(t, drain(t, bus.RegisterBE(1)), "BE sink registered after Close")
}

func TestEventBus_DoubleCloseIsSafe(t *testing.T) {
	bus := newTestBus(t, 1)
	bus.Start()
	closeWithin(t, bus)

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		closeWithin(t, bus)
	}()
	if recovered != nil {
		t.Errorf("Expected a second Close to be a no-op, it panicked with: %v", recovered)
	}
}

func logThroughBus(t *testing.T, e event.Event) string {
	t.Helper()
	var buf bytes.Buffer
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bus, err := event.NewBus(4, logger)
	if err != nil {
		t.Fatalf("NewBus failed: %v", err)
	}
	bus.Start()
	sendWithin(t, bus, []event.Event{e})
	closeWithin(t, bus)

	mu.Lock()
	defer mu.Unlock()
	for line := range strings.SplitSeq(buf.String(), "\n") {
		if strings.Contains(line, e.Message) {
			return line
		}
	}
	t.Fatalf("Expected the log %q in the output, got %q", e.Message, buf.String())
	return ""
}

func TestEventBus_LogRendersIdentityFromFields(t *testing.T) {
	line := logThroughBus(t, event.Event{
		Kind:     event.Log,
		Level:    slog.LevelWarn,
		Message:  "identified",
		RunId:    event.RunId{SeqId: 7, MatId: 2, CntId: 1},
		WorkerId: 3,
		Source:   event.Runner,
		Attrs:    []slog.Attr{slog.String("url", "http://x")},
	})

	for _, want := range []string{"system=" + event.Runner.String(), "workerId=3", "runId.seqId=7", "runId.matrixId=2", "runId.countId=1", "url=http://x"} {
		if !strings.Contains(line, want) {
			t.Errorf("Expected %q in the log line, got %q", want, line)
		}
	}
	if strings.Index(line, "workerId=") > strings.Index(line, "url=") {
		t.Errorf("Expected identity before the log's own attrs, got %q", line)
	}
}

func TestEventBus_LogOmitsEmptyIdentity(t *testing.T) {
	line := logThroughBus(t, event.Event{
		Kind:    event.Log,
		Level:   slog.LevelWarn,
		Message: "anonymous",
		Source:  event.Treewalker,
	})

	if !strings.Contains(line, "system="+event.Treewalker.String()) {
		t.Errorf("Expected the system even without other identity, got %q", line)
	}
	if strings.Contains(line, "workerId=") || strings.Contains(line, "runId") {
		t.Errorf("Expected no workerId or runId for a zero WorkerId and RunId, got %q", line)
	}
}

func TestEventBus_LogRenderingLeavesAttrsAlone(t *testing.T) {
	attrs := make([]slog.Attr, 1, 8)
	attrs[0] = slog.String("own", "attr")
	sink := make(chan []slog.Attr, 1)

	var buf bytes.Buffer
	var mu sync.Mutex
	bus, err := event.NewBus(4, slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewBus failed: %v", err)
	}
	ch := bus.RegisterMC(4)
	go func() {
		for e := range ch {
			sink <- e.Attrs
		}
		close(sink)
	}()
	bus.Start()
	sendWithin(t, bus, []event.Event{{Kind: event.Log, Level: slog.LevelWarn, Message: "m", WorkerId: 1, RunId: event.RunId{SeqId: 1}, Source: event.Runner, Attrs: attrs}})
	closeWithin(t, bus)

	got := <-sink
	if len(got) != 1 || got[0].Key != "own" {
		t.Errorf("Expected the sink to get the log's own attrs untouched, got %v", got)
	}
	if spare := attrs[:cap(attrs)]; spare[1].Key != "" {
		t.Errorf("Expected rendering not to write into the attrs' spare capacity, found %v", spare[1])
	}
}
