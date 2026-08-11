// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/spinq"
)

var ColorStates []string = []string{spinq.Red, spinq.Yellow, spinq.Green, spinq.Cyan, spinq.Blue, spinq.Magenta}

func GetFrame(color bool, clock timer.Clock) spinq.FrameFunc {
	dotsSpinner := spinq.Simple(spinq.DotsStates)
	if color {
		dotsSpinner = spinq.Join("", spinq.SimpleOnceEvery(ColorStates, len(spinq.DotsStates)), dotsSpinner, spinq.Static(spinq.ResetColor))
	}
	return spinq.Join("",
		spinq.Surrounded(" ", dotsSpinner, " Running ("),
		spinq.Duration(clock.Now),
		spinq.Static(")"),
	)
}
