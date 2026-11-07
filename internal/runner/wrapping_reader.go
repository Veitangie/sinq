// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package runner

import (
	"io"

	"veitangie.dev/sinq/internal/sinqerror"
)

type wrappingReader struct {
	wrapped io.Reader
}

func (w wrappingReader) Read(buf []byte) (int, error) {
	c, err := w.wrapped.Read(buf)
	if err != nil && err != io.EOF {
		err = sinqerror.WrapNetworkError(err)
	}
	return c, err
}
