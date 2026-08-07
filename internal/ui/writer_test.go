// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Veitangie/sinq/internal/timer"
)

func TestUIWriterPair(t *testing.T) {
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}

	uiw, spw := MakePair(stdoutBuf, stderrBuf)
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
	uiw, spw := MakePair(ew, ew)

	_, err := uiw.Write([]byte("test"))
	if err != ew.err {
		t.Fatalf("Expected error %v from writer.Write, got %v", ew.err, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	spw.StartSpinner(ctx, timer.DefaultClock{})
	time.Sleep(150 * time.Millisecond)
	cancel()
	spw.Close()

	uiw2, _ := MakePair(ew, ew)
	uiw2.clearer.needClearing = true
	_, err2 := uiw2.Write([]byte("test"))
	if err2 != ew.err {
		t.Errorf("Expected error %v from clear(), got %v", ew.err, err2)
	}
}
