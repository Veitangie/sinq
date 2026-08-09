// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scanner

import "io"

type ByteScanner struct {
	source       []byte
	Current      int
	LineNumber   int
	OffsetNumber int
}

func NewByteScanner(source []byte) *ByteScanner {
	return &ByteScanner{source: source, LineNumber: 1, OffsetNumber: 1}
}

func (b *ByteScanner) Advance() {
	if b.Current >= len(b.source) {
		return
	}
	current := b.source[b.Current]
	b.OffsetNumber++

	if current == '\n' {
		b.LineNumber++
		b.OffsetNumber = 1
	}

	b.Current++
}

func (b *ByteScanner) Read() (byte, error) {
	if b.Current >= len(b.source) {
		return 0b0, io.EOF
	}
	return b.source[b.Current], nil
}

func (b *ByteScanner) Previous() byte {
	if b.Current-1 < 0 || b.Current-1 >= len(b.source) {
		return 0b0
	}
	return b.source[b.Current-1]
}

func (b *ByteScanner) Slice(from, to int) []byte {
	if to == -1 {
		to = len(b.source)
	}
	if from < 0 || from > to || to > len(b.source) {
		return []byte{}
	}
	return b.source[from:to]
}

func (b *ByteScanner) Len() int {
	return len(b.source)
}

func (b *ByteScanner) Left() int {
	return len(b.source) - b.Current
}
