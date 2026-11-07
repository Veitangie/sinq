// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package sinqerror

import (
	"context"
	"errors"
)

var ErrRequestTimeout NetworkError = NetworkError{errors.New("request timeout")}
var ErrTooManyRedirects = errors.New("Too many redirects")

type NetworkError struct {
	wrapped error
}

func WrapNetworkError(wrapped error) error {
	return NetworkError{wrapped}
}

func WrapContextTimeoutErr(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(ctx), ErrRequestTimeout) {
		return ErrRequestTimeout
	}
	return NetworkError{err}
}

func SwapContextTimeoutErr(ctx context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(ctx), ErrRequestTimeout) {
		return ErrRequestTimeout
	}
	return err
}

func (ne NetworkError) Error() string {
	return ne.wrapped.Error()
}

func (ne NetworkError) Unwrap() error {
	return ne.wrapped
}
