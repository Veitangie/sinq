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
