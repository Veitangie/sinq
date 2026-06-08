// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scenario

type TokenType int

const (
	IncompleteToken TokenType = iota
	Text
	Script
	EOF
)

type Token struct {
	Type         TokenType
	Name         string
	Line         int
	Offset       int
	Start        int
	End          int
	PayloadStart int
	PayloadEnd   int
	HasEscapes   bool
}

func (t Token) IsSpecialScript() bool {
	return t.Name == "PRE" || t.Name == "RETRY" || t.Name == "ASSERT" || t.Name == "POST"
}
