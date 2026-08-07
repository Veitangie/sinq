// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"testing"
)

func TestPrefixedWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	pw := &prefixedWriter{prefix: []byte(" ┃ "), underlying: buf}

	_, err := pw.Write([]byte("First line\nSec"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pw.Write([]byte("ond line\n\nFourth line\n"))
	if err != nil {
		t.Fatal(err)
	}

	expected := " ┃ First line\n ┃ Second line\n ┃ \n ┃ Fourth line\n"
	if buf.String() != expected {
		t.Errorf("Prefixed writer failed empty line preservation.\nExpected:\n%q\nGot:\n%q", expected, buf.String())
	}
}

func TestUnsafeSplit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"Empty string", "", []string{""}},
		{"No newline", "hello", []string{"hello"}},
		{"Trailing newline", "hello\n", []string{"hello\n", ""}},
		{"Multiple newlines", "hello\nworld\n", []string{"hello\n", "world\n", ""}},
		{"Only newlines", "\n\n", []string{"\n", "\n", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := unsafeSplit([]byte(tt.input))
			if len(res) != len(tt.expected) {
				t.Fatalf("Expected %d chunks, got %d", len(tt.expected), len(res))
			}
			for i, chunk := range res {
				if string(chunk) != tt.expected[i] {
					t.Errorf("Chunk %d mismatch. Expected %q, got %q", i, tt.expected[i], string(chunk))
				}
			}
		})
	}
}
