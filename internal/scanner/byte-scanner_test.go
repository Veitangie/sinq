// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package scanner

import (
	"io"
	"testing"
)

func TestNewByteScanner_InitialState(t *testing.T) {
	s := NewByteScanner([]byte("abc"))

	if s.Current != 0 {
		t.Errorf("expected Current 0, got %d", s.Current)
	}
	if s.LineNumber != 1 {
		t.Errorf("expected LineNumber 1, got %d", s.LineNumber)
	}
	if s.OffsetNumber != 1 {
		t.Errorf("expected OffsetNumber 1, got %d", s.OffsetNumber)
	}
}

func TestByteScanner_Read(t *testing.T) {
	s := NewByteScanner([]byte("ab"))

	b, err := s.Read()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if b != 'a' {
		t.Errorf("expected 'a', got %q", b)
	}

	b2, err := s.Read()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if b2 != 'a' {
		t.Errorf("expected Read() to be idempotent and still return 'a', got %q", b2)
	}
}

func TestByteScanner_Read_EOF(t *testing.T) {
	s := NewByteScanner([]byte("a"))
	s.Advance()

	_, err := s.Read()
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got %v", err)
	}
}

func TestByteScanner_Read_EmptySource(t *testing.T) {
	s := NewByteScanner([]byte{})

	_, err := s.Read()
	if err != io.EOF {
		t.Fatalf("expected io.EOF for an empty source, got %v", err)
	}
}

func TestByteScanner_Advance_PastEOFIsNoop(t *testing.T) {
	s := NewByteScanner([]byte("a"))
	s.Advance()
	if s.Current != 1 {
		t.Fatalf("expected Current 1 after advancing past 'a', got %d", s.Current)
	}

	s.Advance()
	s.Advance()
	if s.Current != 1 {
		t.Errorf("expected Current to stay at 1 once past EOF, got %d", s.Current)
	}
}

func TestByteScanner_Advance_LineAndOffsetTracking(t *testing.T) {
	s := NewByteScanner([]byte("ab\ncd"))

	type want struct{ current, line, offset int }
	steps := []want{
		{1, 1, 2}, // consumed 'a'
		{2, 1, 3}, // consumed 'b'
		{3, 2, 1}, // consumed '\n' -> new line, offset reset
		{4, 2, 2}, // consumed 'c'
		{5, 2, 3}, // consumed 'd'
	}

	for i, w := range steps {
		s.Advance()
		if s.Current != w.current || s.LineNumber != w.line || s.OffsetNumber != w.offset {
			t.Fatalf("step %d: got {Current:%d Line:%d Offset:%d}, want {%d %d %d}",
				i, s.Current, s.LineNumber, s.OffsetNumber, w.current, w.line, w.offset)
		}
	}
}

func TestByteScanner_Previous(t *testing.T) {
	s := NewByteScanner([]byte("ab"))

	if p := s.Previous(); p != 0 {
		t.Errorf("expected 0 before any advance, got %q", p)
	}

	s.Advance()
	if p := s.Previous(); p != 'a' {
		t.Errorf("expected 'a' after consuming it, got %q", p)
	}

	s.Advance()
	if p := s.Previous(); p != 'b' {
		t.Errorf("expected 'b' after consuming it, got %q", p)
	}
}

func TestByteScanner_Slice(t *testing.T) {
	s := NewByteScanner([]byte("hello world"))

	tests := []struct {
		name     string
		from, to int
		want     string
	}{
		{"normal range", 0, 5, "hello"},
		{"mid range", 6, 11, "world"},
		{"to end via -1 sentinel", 6, -1, "world"},
		{"whole source via -1", 0, -1, "hello world"},
		{"empty range when from == to", 3, 3, ""},
		{"negative from is invalid", -1, 5, ""},
		{"from beyond to is invalid", 5, 2, ""},
		{"to beyond source length is invalid", 0, 100, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.Slice(tt.from, tt.to)
			if string(got) != tt.want {
				t.Errorf("Slice(%d, %d) = %q, want %q", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestByteScanner_Slice_InvalidRangeReturnsNonNilEmpty(t *testing.T) {
	s := NewByteScanner([]byte("abc"))
	got := s.Slice(-1, 5)
	if got == nil {
		t.Error("expected an empty, non-nil slice for an invalid range, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected an empty slice, got %v", got)
	}
}

func TestByteScanner_Len(t *testing.T) {
	s := NewByteScanner([]byte("hello"))
	if s.Len() != 5 {
		t.Errorf("expected Len() 5, got %d", s.Len())
	}

	s.Advance()
	s.Advance()
	if s.Len() != 5 {
		t.Errorf("expected Len() to stay 5 after advancing, got %d", s.Len())
	}
}

func TestByteScanner_Len_EmptySource(t *testing.T) {
	s := NewByteScanner([]byte{})
	if s.Len() != 0 {
		t.Errorf("expected Len() 0 for an empty source, got %d", s.Len())
	}
}

func TestByteScanner_Left(t *testing.T) {
	s := NewByteScanner([]byte("abc"))

	if s.Left() != 3 {
		t.Errorf("expected Left() 3 at the start, got %d", s.Left())
	}

	s.Advance()
	if s.Left() != 2 {
		t.Errorf("expected Left() 2 after one advance, got %d", s.Left())
	}

	s.Advance()
	s.Advance()
	if s.Left() != 0 {
		t.Errorf("expected Left() 0 once fully consumed, got %d", s.Left())
	}

	s.Advance()
	if s.Left() != 0 {
		t.Errorf("expected Left() to stay 0 past EOF, got %d", s.Left())
	}
}
