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
	Reset        = "\033[0m"
	Red          = "\033[31m"
	Yellow       = "\033[33m"
	Green        = "\033[32m"
	Cyan         = "\033[0;36m"
	Blue         = "\033[34m"
	Magenta      = "\033[0;35m"
	Gray         = "\033[90m"
	LightGray    = "\033[38;5;244m"
	ClearSpinner = "\r" + ClearLine
)

var SpinnerStates []string = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
var ColorStates []string = []string{Red, Yellow, Green, Cyan, Blue, Magenta}
var ClearSpinnerBytes []byte = []byte(ClearSpinner)

type spinnerState struct {
	needClearing bool
	canWrite     bool
	writer       io.Writer
	state        []byte
}

func (c *spinnerState) clear() error {
	if c.needClearing && c.canWrite {
		_, err := c.writer.Write(ClearSpinnerBytes)
		c.needClearing = false
		return err
	}
	return nil
}

func (c *spinnerState) restore() error {
	if c.needClearing || !c.canWrite {
		return nil
	}

	_, err := c.writer.Write(c.state)
	c.needClearing = err == nil
	return err
}

func (c *spinnerState) set(state []byte) error {
	err := c.clear()
	if err != nil {
		return err
	}

	c.state = state
	return c.restore()
}

type UIWriter struct {
	mut          *sync.Mutex
	writer       io.Writer
	spinnerState *spinnerState
	wg           *sync.WaitGroup
}

type SpinnerWriter struct {
	UIWriter
	color bool
}

func MakePair(out, err io.Writer, color bool) (*UIWriter, *SpinnerWriter) {
	clearer := &spinnerState{writer: err}
	mut := &sync.Mutex{}
	wg := &sync.WaitGroup{}

	return &UIWriter{mut: mut, writer: out, spinnerState: clearer, wg: wg}, &SpinnerWriter{UIWriter: UIWriter{mut: mut, writer: err, spinnerState: clearer, wg: wg}, color: color}
}

var _ io.Writer = &UIWriter{}
var _ io.Writer = &SpinnerWriter{}

func (w *UIWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	w.mut.Lock()
	defer w.mut.Unlock()
	err := w.spinnerState.clear()
	if err != nil {
		return 0, err
	}

	written, err := w.writer.Write(data)
	if err != nil {
		return written, err
	}

	w.spinnerState.canWrite = false
	if written == len(data) && data[len(data)-1] == '\n' {
		w.spinnerState.canWrite = true
		return written, w.spinnerState.restore()
	}
	return written, err
}

func (w *SpinnerWriter) Close() error {
	w.wg.Wait()

	w.mut.Lock()
	err := w.spinnerState.clear()
	w.mut.Unlock()
	return err
}

func (w *SpinnerWriter) StartSpinner(ctx context.Context, clock timer.Clock) {
	idx := 0
	color := 0
	colorSlice := []string{""}
	reset := ""
	if w.color {
		colorSlice = ColorStates
		reset = Reset
	}

	w.mut.Lock()
	w.spinnerState.canWrite = true
	err := w.spinnerState.set(
		fmt.Appendf(
			nil,
			" %s%s%s Running (0.0s)",
			colorSlice[color],
			SpinnerStates[idx],
			reset,
		),
	)
	idx = (idx + 1) % len(SpinnerStates)
	if idx == 0 {
		color = (color + 1) % len(colorSlice)
	}

	w.mut.Unlock()
	if err != nil {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	duration := timer.NewTimer(clock)
	duration.Start()

	w.wg.Go(func() {
		defer ticker.Stop()
	Loop:
		for {
			select {
			case <-ticker.C:
				w.mut.Lock()
				err := w.spinnerState.set(
					fmt.Appendf(
						nil,
						" %s%s%s Running (%.1fs)",
						colorSlice[color],
						SpinnerStates[idx],
						reset, duration.Time().Seconds(),
					),
				)
				idx = (idx + 1) % len(SpinnerStates)
				if idx == 0 {
					color = (color + 1) % len(colorSlice)
				}

				w.mut.Unlock()
				if err != nil {
					break Loop
				}

			case <-ctx.Done():
				break Loop
			}
		}
	})
}
