// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package standard

import (
	"bytes"
	"io"
)

type prefixedWriter struct {
	prefix      []byte
	underlying  io.Writer
	lastWritten byte
}

var _ io.Writer = &prefixedWriter{}

func (tw *prefixedWriter) Write(data []byte) (int, error) {
	split := bytes.Split(data, []byte{'\n'})
	total := 0
	var err error
	for idx, toWrite := range split {
		if len(toWrite) == 0 && idx == len(split)-1 {
			continue
		}
		if tw.lastWritten == '\n' || tw.lastWritten == 0 {
			_, err = tw.underlying.Write(tw.prefix)
		}
		if err != nil {
			return total, err
		}

		written, err := tw.underlying.Write(toWrite)
		total += written
		if written > 0 && written <= len(toWrite) {
			tw.lastWritten = toWrite[written-1]
		}
		if err != nil {
			return total, err
		}

		if idx != len(split)-1 {
			written, err = tw.underlying.Write([]byte{'\n'})

			total += written
			if err == nil && written == 1 {
				tw.lastWritten = '\n'
			}
		}
	}
	return total, err
}
