// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
)

func main() {
	os.Exit(sinq(os.Args[1:]))
}
