package pgretry

import (
	stderrors "errors"
	"strings"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// IsRetryable decides which failures a Retrier is allowed to re-run. Getting it
// wrong in either direction is expensive: too narrow and a retryable deadlock
// becomes a failed request, too wide and a permanent error is re-run until the
// attempt ceiling.
func TestIsRetryable(T *testing.T) {
	T.Parallel()

	cases := map[string]struct {
		err      error
		expected bool
	}{
		"a deadlock":                 {&pgconn.PgError{Code: pgDeadlockDetected}, true},
		"a serialization failure":    {&pgconn.PgError{Code: pgSerializationFailure}, true},
		"a wrapped deadlock":         {platformerrors.Wrap(&pgconn.PgError{Code: pgDeadlockDetected}, "writing"), true},
		"another Postgres condition": {&pgconn.PgError{Code: "23505"}, false},
		"an unrelated error":         {stderrors.New("boom"), false},
		"no error at all":            {nil, false},
	}

	for name, tc := range cases {
		T.Run(name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.expected, IsRetryable(tc.err))
		})
	}
}

func TestTruncateError(T *testing.T) {
	T.Parallel()

	// The column is nullable and the row distinguishes "has not failed" from
	// "failed"; rendering nil as "" would collapse the two.
	T.Run("a nil cause stores nothing", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, TruncateError(nil))
	})

	T.Run("a short cause reaches the column intact", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "boom", TruncateError(platformerrors.New("boom")))
	})

	T.Run("bounds what reaches the column", func(t *testing.T) {
		t.Parallel()

		stored, ok := TruncateError(stderrors.New(strings.Repeat("e", MaxStoredErrLen*2))).(string)

		must.True(t, ok)
		test.EqOp(t, MaxStoredErrLen, len(stored))
	})
}

func TestRetrier_Do(T *testing.T) {
	T.Parallel()

	deadlock := &pgconn.PgError{Code: pgDeadlockDetected}

	T.Run("runs the write once when it succeeds", func(t *testing.T) {
		t.Parallel()

		var calls int

		r := &Retrier{Attempts: 5}

		err := r.Do(t.Context(), "writing", func() error {
			calls++

			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 1, calls)
	})

	// The point of the classification: a permanent failure must not be re-run,
	// because re-asking an answered question only delays the error.
	T.Run("returns a non-retryable failure without re-running it", func(t *testing.T) {
		t.Parallel()

		var calls int

		sentinel := stderrors.New("constraint violated")
		r := &Retrier{Attempts: 5}

		err := r.Do(t.Context(), "writing", func() error {
			calls++

			return sentinel
		})

		test.ErrorIs(t, err, sentinel)
		test.EqOp(t, 1, calls)
	})

	T.Run("re-runs a deadlock until it succeeds", func(t *testing.T) {
		t.Parallel()

		var calls int

		r := &Retrier{Attempts: 5}

		err := r.Do(t.Context(), "writing", func() error {
			calls++
			if calls == 1 {
				return deadlock
			}

			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 2, calls)
	})

	// Attempts is a ceiling, not a suggestion — a deadlock that never clears has
	// to stop rather than spin.
	T.Run("gives up at the attempt ceiling and returns the last failure", func(t *testing.T) {
		t.Parallel()

		var calls int

		r := &Retrier{Attempts: 3}

		err := r.Do(t.Context(), "writing", func() error {
			calls++

			return deadlock
		})

		test.ErrorIs(t, err, deadlock)
		test.EqOp(t, 3, calls)
	})

	// The zero value has to be usable rather than panic on the absent logger and
	// counter, because "no attempt budget was configured" is a legible state.
	T.Run("the zero value runs the write once and retries nothing", func(t *testing.T) {
		t.Parallel()

		var calls int

		var r Retrier

		err := r.Do(t.Context(), "writing", func() error {
			calls++

			return deadlock
		})

		test.ErrorIs(t, err, deadlock)
		test.EqOp(t, 1, calls)
	})
}
