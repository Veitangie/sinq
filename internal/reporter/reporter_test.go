// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package reporter

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/runner"
)

type mockReporter struct {
	id            int
	receivedCount atomic.Int32
}

func (m *mockReporter) Report(source <-chan runner.ScenarioResult, timer <-chan time.Duration, size int) error {
	for range source {
		m.receivedCount.Add(1)
	}
	<-timer
	return nil
}

func TestReporterPool_ConcurrencyRaceAndDelivery(t *testing.T) {
	rep1 := &mockReporter{id: 1}
	rep2 := &mockReporter{id: 2}
	rep3 := &mockReporter{id: 3}

	pool := NewPool(rep1, rep2, rep3)

	sourceCh := make(chan runner.ScenarioResult, 50)
	timerCh := make(chan time.Duration, 1)

	payloadCount := 1000

	go func() {
		for range payloadCount {
			sourceCh <- runner.ScenarioResult{Name: "RaceTestPayload"}
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
