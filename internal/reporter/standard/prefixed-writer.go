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

func unsafeSplit(data []byte) [][]byte {
	if len(data) == 0 {
		return [][]byte{{}}
	}
	res := make([][]byte, 0, bytes.Count(data, []byte{'\n'})+1)
	start := 0
	for idx, b := range data {
		if b == '\n' {
			res = append(res, data[start:idx+1])
			start = idx + 1
		}
	}
	res = append(res, data[start:])

	return res
}

func (tw *prefixedWriter) Write(data []byte) (int, error) {
	split := unsafeSplit(data)
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
	}
	return total, err
}
