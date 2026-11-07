// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package reporter

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/event/sink/aggregator"
)

type mockReporter struct {
	id            int
	err           error
	receivedCount atomic.Int32
}

func (m *mockReporter) Report(source <-chan aggregator.ScenarioResult, timer <-chan time.Duration, size int) error {
	for range source {
		m.receivedCount.Add(1)
	}
	<-timer
	return m.err
}

func TestReporterPool_ConcurrencyRaceAndDelivery(t *testing.T) {
	rep1 := &mockReporter{id: 1}
	rep2 := &mockReporter{id: 2}
	rep3 := &mockReporter{id: 3}

	pool := NewPool(rep1, rep2, rep3)

	sourceCh := make(chan aggregator.ScenarioResult, 50)
	timerCh := make(chan time.Duration, 1)

	payloadCount := 1000

	go func() {
		for range payloadCount {
			sourceCh <- aggregator.ScenarioResult{Name: "RaceTestPayload"}
		}
		close(sourceCh)
		timerCh <- 1 * time.Second
		close(timerCh)
	}()

	err := pool.Report(sourceCh, timerCh, payloadCount)
	if err != nil {
		t.Fatalf("ReporterPool failed unexpectedly: %v", err)
	}

	if rep1.receivedCount.Load() != int32(payloadCount) {
		t.Errorf("Reporter 1 dropped payloads. Expected %d, got %d", payloadCount, rep1.receivedCount.Load())
	}
	if rep2.receivedCount.Load() != int32(payloadCount) {
		t.Errorf("Reporter 2 dropped payloads. Expected %d, got %d", payloadCount, rep2.receivedCount.Load())
	}
	if rep3.receivedCount.Load() != int32(payloadCount) {
		t.Errorf("Reporter 3 dropped payloads. Expected %d, got %d", payloadCount, rep3.receivedCount.Load())
	}
}

func TestReporterPool_Register(t *testing.T) {
	pool := NewPool()

	err := pool.Register(nil)
	if err == nil {
		t.Error("Expected error when registering nil reporter")
	}

	var wg sync.WaitGroup
	numReporters := 100

	for i := range numReporters {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := pool.Register(&mockReporter{id: id})
			if err != nil {
				t.Errorf("Unexpected error when registering valid reporter: %v", err)
			}
		}(i)
	}

	wg.Wait()

	pool.lock.RLock()
	defer pool.lock.RUnlock()
	if len(pool.reporters) != numReporters {
		t.Errorf("Expected pool to have %d reporters, got %d", numReporters, len(pool.reporters))
	}
}

func TestReporterPool_PropagatesReporterError(t *testing.T) {
	wantErr := errors.New("reporter blew up")
	good := &mockReporter{id: 1}
	bad := &mockReporter{id: 2, err: wantErr}

	pool := NewPool(good, bad)

	sourceCh := make(chan aggregator.ScenarioResult, 1)
	timerCh := make(chan time.Duration, 1)
	sourceCh <- aggregator.ScenarioResult{Name: "Payload"}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	err := pool.Report(sourceCh, timerCh, 1)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected pool.Report to propagate the reporter's error, got: %v", err)
	}
}
