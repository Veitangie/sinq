// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/timer"
)

func TestUIWriterPair(t *testing.T) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	uiw, spw := MakePair(stdoutBuf, stderrBuf, false)
	_, err := spw.Write([]byte("slog warn\n"))
	if err != nil {
		t.Fatalf("spw.Write error: %v", err)
	}

	if stderrBuf.String() != "slog warn\n" {
		t.Errorf("Expected 'slog warn\\n' in stderrBuf, got %q", stderrBuf.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	spw.StartSpinner(ctx, timer.DefaultClock{})

	time.Sleep(150 * time.Millisecond)

	_, err = uiw.Write([]byte("reporter success\n"))
	if err != nil {
		t.Fatalf("uiw.Write error: %v", err)
	}

	cancel()
	spw.Close()

	outStr := stdoutBuf.String()
	errStr := stderrBuf.String()

	if outStr != "reporter success\n" {
		t.Errorf("Expected exactly 'reporter success\\n' in stdout, got %q", outStr)
	}

	if !strings.Contains(errStr, string(ClearSpinnerBytes)) {
		t.Errorf("Expected stderr to contain ClearSpinnerBytes, got %q", errStr)
	}
	if !strings.HasSuffix(errStr, string(ClearSpinnerBytes)) {
		t.Errorf("Expected stderr to end with ClearSpinnerBytes from Close(), got %q", errStr)
	}
}

type errorWriter struct {
	err error
}

func (e *errorWriter) Write(p []byte) (n int, err error) {
	return 0, e.err
}

func TestUIWriter_Errors(t *testing.T) {
	ew := &errorWriter{err: context.DeadlineExceeded}
	uiw, spw := MakePair(ew, ew, false)

	_, err := uiw.Write([]byte("test"))
	if err != ew.err {
		t.Fatalf("Expected error %v from writer.Write, got %v", ew.err, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	spw.StartSpinner(ctx, timer.DefaultClock{})
	time.Sleep(150 * time.Millisecond)
	cancel()
	spw.Close()

	uiw2, _ := MakePair(ew, ew, false)
	uiw2.spinnerState.needClearing = true
	_, err2 := uiw2.Write([]byte("test"))
	if err2 != ew.err {
		t.Errorf("Expected error %v from clear(), got %v", ew.err, err2)
	}
}

// TestUIWriter_ConcurrentStress hammers the shared writer/spinner from many
// goroutines at once (mirroring real usage: many scenario workers writing
// results while the spinner ticks) to catch races the single-goroutine tests
// above can't reach. Run with -race.
func TestUIWriter_ConcurrentStress(t *testing.T) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	uiw, spw := MakePair(stdoutBuf, stderrBuf, true)
	ctx, cancel := context.WithCancel(context.Background())
	spw.StartSpinner(ctx, timer.DefaultClock{})

	var wg sync.WaitGroup
	const uiWorkers, uiLines = 50, 50
	for i := range uiWorkers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range uiLines {
				fmt.Fprintf(uiw, "worker %d line %d\n", n, j)
			}
		}(i)
	}

	const logWorkers, logLines = 10, 20
	for i := range logWorkers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range logLines {
				fmt.Fprintf(spw, "log %d %d\n", n, j)
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	cancel()
	if err := spw.Close(); err != nil {
		t.Fatalf("spw.Close() error: %v", err)
	}
}

// TestUIWriter_ConcurrentStress_Integrity checks that concurrent writers
// racing on the shared mutex never produce torn, merged, duplicated, or
// dropped lines - race-safety alone (see above) doesn't guarantee that.
func TestUIWriter_ConcurrentStress_Integrity(t *testing.T) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	uiw, spw := MakePair(stdoutBuf, stderrBuf, true)
	ctx, cancel := context.WithCancel(context.Background())
	spw.StartSpinner(ctx, timer.DefaultClock{})

	var wg sync.WaitGroup
	const workers, lines = 50, 50
	for i := range workers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := range lines {
				fmt.Fprintf(uiw, "worker %d line %d\n", n, j)
			}
		}(i)
	}
	wg.Wait()
	cancel()
	if err := spw.Close(); err != nil {
		t.Fatalf("spw.Close() error: %v", err)
	}

	lineRe := regexp.MustCompile(`^worker \d+ line \d+$`)
	seen := map[string]int{}
	for l := range bytes.SplitSeq(stdoutBuf.Bytes(), []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		if !lineRe.Match(l) {
			t.Fatalf("corrupted/torn line found: %q", l)
		}
		seen[string(l)]++
	}
	if len(seen) != workers*lines {
		t.Fatalf("expected %d distinct lines, got %d (dupes or losses)", workers*lines, len(seen))
	}
	for k, c := range seen {
		if c != 1 {
			t.Fatalf("line %q appeared %d times, expected 1", k, c)
		}
	}
}
