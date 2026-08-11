package retry

import (
	"context"
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestUnretryable(T *testing.T) {
	T.Parallel()

	T.Run("marks an error unretryable while preserving the chain", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("underlying")
		err := Unretryable(sentinel)

		must.Error(t, err)
		test.ErrorIs(t, err, ErrUnretryable)
		test.ErrorIs(t, err, sentinel)
	})

	T.Run("nil stays nil", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, Unretryable(nil))
	})

	T.Run("wrapping twice still matches both", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("underlying")
		err := Unretryable(Unretryable(sentinel))

		test.ErrorIs(t, err, ErrUnretryable)
		test.ErrorIs(t, err, sentinel)
	})
}

func TestIsTerminal(T *testing.T) {
	T.Parallel()

	T.Run("an ordinary error is retryable", func(t *testing.T) {
		t.Parallel()

		test.False(t, IsTerminal(t.Context(), errors.New("transient")))
	})

	T.Run("nil error with a live context is not terminal", func(t *testing.T) {
		t.Parallel()

		test.False(t, IsTerminal(t.Context(), nil))
	})

	T.Run("an explicitly unretryable error is terminal", func(t *testing.T) {
		t.Parallel()

		test.True(t, IsTerminal(t.Context(), Unretryable(errors.New("fatal"))))
	})

	T.Run("a canceled loop context is terminal", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		test.True(t, IsTerminal(ctx, errors.New("transient")))
	})

	T.Run("a per-attempt deadline is not terminal", func(t *testing.T) {
		t.Parallel()

		// The operation bounded itself and blew its own deadline. The retry
		// loop's context is untouched, so another attempt can still succeed —
		// this is the case the ctx-not-err check exists to keep retryable.
		attemptCtx, cancel := context.WithTimeout(t.Context(), 0)
		defer cancel()
		<-attemptCtx.Done()

		test.False(t, IsTerminal(t.Context(), attemptCtx.Err()))
	})
}

func TestExhausted(T *testing.T) {
	T.Parallel()

	T.Run("carries the count and keeps the cause matchable", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("connection refused")

		err := Exhausted(4, cause)
		must.Error(t, err)
		test.ErrorIs(t, err, cause)
		test.ErrorIs(t, err, ErrExhausted)

		attempts, ok := Attempts(err)
		must.True(t, ok)
		test.EqOp(t, uint(4), attempts)
	})

	T.Run("a loop that succeeded exhausted nothing", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, Exhausted(3, nil))
	})

	T.Run("an ordinary error does not know its attempt count", func(t *testing.T) {
		t.Parallel()

		attempts, ok := Attempts(errors.New("connection refused"))
		test.False(t, ok)
		test.EqOp(t, uint(0), attempts)

		attempts, ok = Attempts(nil)
		test.False(t, ok)
		test.EqOp(t, uint(0), attempts)
	})

	T.Run("says how many attempts in its message", func(t *testing.T) {
		t.Parallel()

		test.StrContains(t, Exhausted(4, errors.New("nope")).Error(), "after 4 attempts")
	})
}
