// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package spinner

import (
	"bytes"
	"context"
	"testing"
	"time"

	"veitangie.dev/sinq/internal/runner"
	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/spinq"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

var _ timer.Clock = &mockClock{}

func TestNewSpinnerReporter_NilSpinner(t *testing.T) {
	_, err := NewSpinnerReporter(nil, timer.DefaultClock{}, 10, false)
	if err == nil {
		t.Fatal("expected an error for a nil spinner, got nil")
	}
}

func TestNewSpinnerReporter_NilClock_DefaultsWithoutError(t *testing.T) {
	spinner := spinq.WriterPassthrough{Writer: &bytes.Buffer{}}
	rep, err := NewSpinnerReporter(spinner, nil, 10, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.clock == nil {
		t.Error("expected a nil clock to be defaulted, got nil")
	}
}

func TestSpinnerReporter_Report_SimpleMode_DrainsResults(t *testing.T) {
	spinner := spinq.WriterPassthrough{Writer: &bytes.Buffer{}}
	rep, err := NewSpinnerReporter(spinner, timer.DefaultClock{}, 5, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sourceCh := make(chan runner.ScenarioResult, 2)
	timerCh := make(chan time.Duration, 1)
	sourceCh <- runner.ScenarioResult{Status: runner.Success}
	sourceCh <- runner.ScenarioResult{Status: runner.Skipped}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpinnerReporter_Report_BarMode_DrainsResults(t *testing.T) {
	spinner := spinq.WriterPassthrough{Writer: &bytes.Buffer{}}
	rep, err := NewSpinnerReporter(spinner, timer.DefaultClock{}, 20, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sourceCh := make(chan runner.ScenarioResult, 4)
	timerCh := make(chan time.Duration, 1)
	sourceCh <- runner.ScenarioResult{Status: runner.Success}
	sourceCh <- runner.ScenarioResult{Status: runner.Skipped}
	sourceCh <- runner.ScenarioResult{Status: runner.Unset}
	sourceCh <- runner.ScenarioResult{Status: runner.Failure}
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 20); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpinnerReporter_Report_SwallowsErrClosedFromStop(t *testing.T) {
	ctx := context.Background()
	pair, err := spinq.WrapPair(ctx, &bytes.Buffer{}, &bytes.Buffer{}, spinq.Static(" "), make(chan time.Time))
	if err != nil {
		t.Fatalf("failed to construct pair: %v", err)
	}
	pair.Spinner.Close()

	rep, err := NewSpinnerReporter(pair.Spinner, timer.DefaultClock{}, 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sourceCh := make(chan runner.ScenarioResult)
	timerCh := make(chan time.Duration, 1)
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("expected ErrClosed from Stop() to be swallowed, got: %v", err)
	}
}

func TestSpinnerReporter_Report_AlwaysStopsSpinner(t *testing.T) {
	ctx := context.Background()
	buf := &bytes.Buffer{}
	pair, err := spinq.WrapPair(ctx, buf, buf, spinq.Static("hello"), make(chan time.Time))
	if err != nil {
		t.Fatalf("failed to construct pair: %v", err)
	}
	defer pair.Spinner.Close()

	if err := pair.Spinner.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	rep, err := NewSpinnerReporter(pair.Spinner, timer.DefaultClock{}, 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sourceCh := make(chan runner.ScenarioResult)
	timerCh := make(chan time.Duration, 1)
	close(sourceCh)
	timerCh <- 1 * time.Millisecond
	close(timerCh)

	if err := rep.Report(sourceCh, timerCh, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	before := buf.Len()
	marker := []byte("MARKER\n")
	if _, err := pair.Spinner.Write(marker); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	written := buf.Bytes()[before:]

	if string(written) != string(marker) {
		t.Errorf("expected only the marker bytes to land (spinner should be stopped after Report()), got %q - the spinner is still running and redrawing", written)
	}
}
