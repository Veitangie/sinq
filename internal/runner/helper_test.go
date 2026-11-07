// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"bytes"
	"context"
	"hash/maphash"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/sync/singleflight"
	"veitangie.dev/sinq/internal/config"
	"veitangie.dev/sinq/internal/event"
	"veitangie.dev/sinq/internal/event/sink/aggregator"
	"veitangie.dev/sinq/internal/scenario"
	"veitangie.dev/sinq/internal/timer"
)

type mockWorkspace struct {
	fs.FS
}

func (m *mockWorkspace) Open(name string) (fs.File, error) {
	return m.FS.Open(name)
}

func (m *mockWorkspace) Stat(name string) (fs.FileInfo, error) {
	return fs.Stat(m.FS, name)
}

func (m *mockWorkspace) MkDirall(name string) error {
	return nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (m *mockWorkspace) Create(name string) (io.WriteCloser, error) {
	buf := &bytes.Buffer{}
	return nopWriteCloser{buf}, nil
}

func (m *mockWorkspace) String() string {
	return "test"
}

const observeTimeout = 2 * time.Second

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestBus(t *testing.T) *event.Bus {
	t.Helper()
	bus, err := event.NewBus(64, discardLogger())
	if err != nil {
		t.Fatalf("Failed to construct event bus: %v", err)
	}
	bus.Start()
	t.Cleanup(bus.Close)
	return bus
}

type observer struct {
	t       *testing.T
	bus     *event.Bus
	results <-chan aggregator.ScenarioResult
	mu      sync.Mutex
	got     []event.Event
	synced  chan struct{}
}

const observerSyncMessage = "__observer_sync__"

func observe(t *testing.T, w *worker) *observer {
	t.Helper()
	bus, err := event.NewBus(64, discardLogger())
	if err != nil {
		t.Fatalf("Failed to construct event bus: %v", err)
	}
	o := &observer{t: t, bus: bus, synced: make(chan struct{}, 1)}

	stop := make(chan struct{})
	aggregated, _ := aggregator.NewAggregator().AggregateOver(bus.RegisterMC(1024))
	results := make(chan aggregator.ScenarioResult, 64)
	go func() {
		for {
			select {
			case r := <-aggregated:
				select {
				case results <- r:
				case <-stop:
					return
				}
			case <-stop:
				return
			}
		}
	}()
	o.results = results

	recorded := bus.RegisterMC(1024)
	go func() {
		for e := range recorded {
			if e.Kind == event.Log && e.Message == observerSyncMessage {
				o.synced <- struct{}{}
				continue
			}
			o.mu.Lock()
			o.got = append(o.got, e)
			o.mu.Unlock()
		}
	}()

	bus.Start()
	t.Cleanup(func() {
		bus.Close()
		close(stop)
	})
	w.env.eb = bus
	if w.env.cachedProcessor != nil {
		w.env.cachedProcessor.eb = bus
	}
	return o
}

func (o *observer) events() []event.Event {
	o.t.Helper()
	o.bus.Send(context.Background(), event.Event{Kind: event.Log, Message: observerSyncMessage})
	select {
	case <-o.synced:
	case <-time.After(observeTimeout):
		o.t.Fatal("Timed out waiting for the event bus to deliver")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]event.Event(nil), o.got...)
}

func (o *observer) ofKind(kind event.EventType) []event.Event {
	o.t.Helper()
	res := []event.Event{}
	for _, e := range o.events() {
		if e.Kind == kind {
			res = append(res, e)
		}
	}
	return res
}

func (o *observer) logged(msg string) bool {
	o.t.Helper()
	for _, e := range o.ofKind(event.Log) {
		if bytes.Contains([]byte(e.Message), []byte(msg)) {
			return true
		}
	}
	return false
}

func (o *observer) awaitLogged(msg string) {
	o.t.Helper()
	deadline := time.After(observeTimeout)
	for !o.logged(msg) {
		select {
		case <-deadline:
			o.t.Fatalf("Timed out waiting for a %q log event", msg)
		case <-time.After(time.Millisecond):
		}
	}
}

func (o *observer) result() aggregator.ScenarioResult {
	o.t.Helper()
	select {
	case res := <-o.results:
		return res
	case <-time.After(observeTimeout):
		o.t.Fatal("Timed out waiting for a scenario result")
	}
	return aggregator.ScenarioResult{}
}

func setupTestWorker(t *testing.T, ctx context.Context) *worker {
	t.Helper()

	if ctx == nil {
		ctx = context.Background()
	}

	bus := newTestBus(t)
	env := workerEnv{
		eb:        bus,
		clock:     timer.DefaultClock{},
		transport: http.DefaultTransport,
		compiler:  &cachedCompiler{hasherSeed: maphash.MakeSeed()},
		workspace: &mockWorkspace{FS: fstest.MapFS{}},
		cachedProcessor: &cachedRequestProcessor{
			group:        singleflight.Group{},
			ctx:          context.Background(),
			transport:    http.DefaultTransport,
			maxCacheSize: config.DataSize{ByteAmount: 1 << 20, Unit: config.MiByte},
			cacheTimeout: 1 * time.Second,
			eb:           bus,
		},
	}

	w := &worker{
		id:        1,
		env:       env,
		reportCtx: ctx,
	}

	err := w.setupScenarioEnvironment(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to setup Lua environment: %v", err)
	}

	t.Cleanup(func() {
		w.Close()
	})

	return w
}

// newTestProcessor returns a RequestProcessor for w with the fields every send needs
// filled in: a background context, a timer, a plain client, a 5s request timeout and
// an empty scenario config. Tests set whatever else they need on the result, so a new
// required field only has to be added here.
func newTestProcessor(w *worker) *RequestProcessor {
	return &RequestProcessor{
		w:            w,
		ctx:          context.Background(),
		scenarioBp:   &scenario.ScenarioBlueprint{Config: &scenario.ScenarioConfig{}},
		requestTimer: timer.NewTimer(timer.DefaultClock{}),
		client:       &http.Client{},
		reqTimeout:   5 * time.Second,
	}
}
