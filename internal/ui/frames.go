// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package ui

import (
	"veitangie.dev/sinq/internal/timer"
	"veitangie.dev/spinq"
)

var ColorStates []string = []string{spinq.Red, spinq.Yellow, spinq.Green, spinq.Cyan, spinq.Blue, spinq.Magenta}

const black = "\033[0;30m"

func GetSimpleFrame(color bool, clock timer.Clock, label string) spinq.FrameFunc {
	dotsSpinner := spinq.Simple(spinq.DotsStates)
	if color {
		dotsSpinner = spinq.Join("", spinq.SimpleOnceEvery(ColorStates, len(spinq.DotsStates)), dotsSpinner, spinq.Static(spinq.ResetColor))
	}
	return spinq.Join("",
		spinq.Surrounded(" ", dotsSpinner, " "+label+" ("),
		spinq.Duration(clock.Now),
		spinq.Static(")\r"),
	)
}

func GetBarFrame(color bool, maxCount int, progress func() (int, int), getWidth func() int) spinq.FrameFunc {
	dotsSpinner := spinq.Simple(spinq.DotsStates)
	barStart := " Running | "
	barEnd := ""
	full := "━"
	divider := "━"
	empty := " "
	if color {
		dotsSpinner = spinq.Join("", spinq.SimpleOnceEvery(ColorStates, len(spinq.DotsStates)), dotsSpinner, spinq.Static(spinq.ResetColor))
		barStart = barStart + spinq.Green
		barEnd = barEnd + spinq.ResetColor
		divider = divider + black
		empty = full
	}
	maxCountDigits := 0
	for maxCount2 := maxCount; maxCount2 > 0; maxCount2 /= 10 {
		maxCountDigits += 1
	}
	maxCountDigits *= 2
	render := spinq.DynamicBarRender(
		spinq.Clamp(spinq.Offset(getWidth, -7-maxCountDigits), 0, 50),
		spinq.BarWithStart(barStart),
		spinq.BarWithFull(full),
		spinq.BarWithDivider(divider),
		spinq.BarWithEmpty(empty),
		spinq.BarWithEnd(barEnd),
	).Join(" | ", spinq.FractRender("/"))
	return spinq.Join("",
		spinq.Static(" "),
		dotsSpinner,
		spinq.Progress(progress, render),
		spinq.Static("\r"),
	)
}
