// sinq - A concurrent integration testing tool
// Copyright (C) 2026 Veitangie
// SPDX-License-Identifier: GPL-3.0-or-later
package sinqerror

import (
	"context"
	"errors"
	"testing"
	"time"
)

func expired(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Expected the context to expire")
	}
}

func TestWrapContextTimeoutErr(t *testing.T) {
	t.Run("request deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeoutCause(context.Background(), time.Millisecond, ErrRequestTimeout)
		defer cancel()
		expired(t, ctx)

		err := WrapContextTimeoutErr(ctx, context.DeadlineExceeded)
		if err != ErrRequestTimeout {
			t.Errorf("Expected ErrRequestTimeout, got %v", err)
		}
	})

	t.Run("scenario deadline first", func(t *testing.T) {
		scenario, cancelScenario := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancelScenario()
		ctx, cancel := context.WithTimeoutCause(scenario, time.Hour, ErrRequestTimeout)
		defer cancel()
		expired(t, ctx)

		err := WrapContextTimeoutErr(ctx, context.DeadlineExceeded)
		if errors.Is(err, ErrRequestTimeout) {
			t.Errorf("Expected the scenario deadline not to become a request timeout, got %v", err)
		}
		if _, ok := errors.AsType[NetworkError](err); !ok || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Expected a NetworkError wrapping the deadline, got %#v", err)
		}
	})

	t.Run("plain network error", func(t *testing.T) {
		ctx, cancel := context.WithTimeoutCause(context.Background(), time.Hour, ErrRequestTimeout)
		defer cancel()
		refused := errors.New("connection refused")

		err := WrapContextTimeoutErr(ctx, refused)
		if _, ok := errors.AsType[NetworkError](err); !ok || !errors.Is(err, refused) {
			t.Errorf("Expected a NetworkError wrapping the original error, got %#v", err)
		}
		if err.Error() != "connection refused" {
			t.Errorf("Expected the wrapped error's text, got %q", err.Error())
		}
	})

	t.Run("deadline error before the request deadline fired", func(t *testing.T) {
		ctx, cancel := context.WithTimeoutCause(context.Background(), time.Hour, ErrRequestTimeout)
		defer cancel()

		if err := WrapContextTimeoutErr(ctx, context.DeadlineExceeded); errors.Is(err, ErrRequestTimeout) {
			t.Errorf("Expected no request timeout while the request context is still live, got %v", err)
		}
	})
}

func TestSwapContextTimeoutErr(t *testing.T) {
	timedOut, cancel := context.WithTimeoutCause(context.Background(), time.Millisecond, ErrRequestTimeout)
	defer cancel()
	expired(t, timedOut)

	if err := SwapContextTimeoutErr(timedOut, WrapNetworkError(context.DeadlineExceeded)); err != ErrRequestTimeout {
		t.Errorf("Expected a wrapped deadline to become ErrRequestTimeout, got %v", err)
	}

	live, cancelLive := context.WithTimeoutCause(context.Background(), time.Hour, ErrRequestTimeout)
	defer cancelLive()
	other := errors.New("permission denied")
	if err := SwapContextTimeoutErr(live, other); err != other {
		t.Errorf("Expected other errors to pass through unchanged, got %#v", err)
	}
}
