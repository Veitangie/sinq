// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Veitangie/sinq/internal/timer"
)

const (
	HideCursor   = "\033[?25l"
	ShowCursor   = "\033[?25h"
	ClearLine    = "\033[K"
	ClearSpinner = HideCursor + "\r" + ClearLine + ShowCursor
)

var SpinnerStates []string = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var ClearSpinnerBytes []byte = []byte(ClearSpinner)

type clearer struct {
	needClearing bool
	writer       io.Writer
}

func (c *clearer) clear() error {
	if c.needClearing {
		_, err := c.writer.Write(ClearSpinnerBytes)
		c.needClearing = false
		return err
	}
	return nil
}

type UIWriter struct {
	mut     *sync.Mutex
	writer  io.Writer
	clearer *clearer
	wg      *sync.WaitGroup
}

type SpinnerWriter struct {
	UIWriter
}

func MakePair(out, err io.Writer) (*UIWriter, *SpinnerWriter) {
	clearer := &clearer{writer: err}
	mut := &sync.Mutex{}
	wg := &sync.WaitGroup{}

	return &UIWriter{mut: mut, writer: out, clearer: clearer, wg: wg}, &SpinnerWriter{UIWriter: UIWriter{mut: mut, writer: err, clearer: clearer, wg: wg}}
}

var _ io.Writer = &UIWriter{}
var _ io.Writer = &SpinnerWriter{}

func (w *UIWriter) Write(data []byte) (int, error) {
	w.mut.Lock()
	defer w.mut.Unlock()
	err := w.clearer.clear()
	if err != nil {
		return 0, err
	}

	return w.writer.Write(data)
}

func (w *SpinnerWriter) Close() {
	w.wg.Wait()

	w.mut.Lock()
	err := w.clearer.clear()
	w.mut.Unlock()
	// This here is just for golangci-lint
	if err != nil {
		return
	}
}

func (w *SpinnerWriter) StartSpinner(ctx context.Context, clock timer.Clock) {
	w.wg.Add(1)
	ticker := time.NewTicker(100 * time.Millisecond)
	duration := timer.NewTimer(clock)
	duration.Start()
	idx := 0

	go func() {

		defer w.wg.Done()
	Loop:
		for {
			select {
			case <-ticker.C:
				w.mut.Lock()
				err := w.clearer.clear()
				if err != nil {
					w.mut.Unlock()
					break Loop
				}
				_, err = fmt.Fprintf(w.writer, " %s Running (%.1fs)", SpinnerStates[idx], duration.Time().Seconds())
				idx = (idx + 1) % len(SpinnerStates)
				w.clearer.needClearing = true
				w.mut.Unlock()
				if err != nil {
					break Loop
				}

			case <-ctx.Done():
				break Loop
			}
		}
	}()
}
