package retry

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnretryable marks an error as one that Execute must not retry. Wrap a
// returned error with Unretryable (or return anything that wraps ErrUnretryable)
// to stop the retry loop immediately instead of exhausting the remaining attempts.
var ErrUnretryable = errors.New("unretryable")

// Unretryable wraps err so Execute stops retrying on it. The original error is
// preserved in the chain, so errors.Is/As against it still work.
func Unretryable(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%w: %w", ErrUnretryable, err)
}

// ErrExhausted marks the error a policy returns when it has spent every attempt
// it was given without the operation succeeding.
//
// It is a distinct answer from the operation's own last error, which it wraps:
// "the database refused the connection" and "the database refused the
// connection five times over four seconds" are different facts about a request,
// and only the second one explains where its latency went. Match it with
// errors.Is; read the count with Attempts.
var ErrExhausted = errors.New("retries exhausted")

// ExhaustedError is what a policy returns when the attempts run out, carrying
// the count that produced it alongside the last error.
//
// Callers rarely construct one — Exhausted does — but the fields are exported so
// a caller matching with errors.As can read the count without a helper.
type ExhaustedError struct {
	// Err is the error the final attempt returned.
	Err error
	// Attempts is how many times the operation ran, including the first.
	Attempts uint
}

func (e *ExhaustedError) Error() string {
	return fmt.Sprintf("%s after %d attempts: %v", ErrExhausted.Error(), e.Attempts, e.Err)
}

// Unwrap returns the last error, so everything a caller could match against
// before the loop gave up still matches.
func (e *ExhaustedError) Unwrap() error { return e.Err }

// Is reports ErrExhausted, so a caller can ask the question without naming the
// type.
func (e *ExhaustedError) Is(target error) bool { return target == ErrExhausted }

// Exhausted wraps err as the end of a loop that ran attempts times.
//
// A nil err is nothing to report: a loop that succeeded did not exhaust itself,
// however many attempts it took.
func Exhausted(attempts uint, err error) error {
	if err == nil {
		return nil
	}

	return &ExhaustedError{Attempts: attempts, Err: err}
}

// Attempts reports how many attempts produced err, and whether err is the kind
// of error that knows.
//
// Only an exhausted loop knows. An operation that failed unretryably, or one
// whose context was canceled mid-loop, returns the error it got — the loop
// stopped for a reason of its own rather than for want of attempts.
func Attempts(err error) (uint, bool) {
	if exhausted, ok := errors.AsType[*ExhaustedError](err); ok {
		return exhausted.Attempts, true
	}

	return 0, false
}

// IsTerminal reports whether an operation error should abort a retry loop
// rather than trigger another attempt: the loop's own context is done, so
// retrying can never succeed, or the error is explicitly non-retryable.
//
// The question is asked of ctx, not of err. Matching err against
// context.DeadlineExceeded treats a *per-attempt* timeout as terminal — and a
// per-attempt timeout is the single most common transient failure there is, so
// the loop gave up on attempt one for exactly the case it exists to survive.
// An operation that bounds itself with its own context.WithTimeout hands back a
// wrapped DeadlineExceeded while this loop's deadline is nowhere near.
//
// It is exported because the policies that consume it live in retry/config,
// and because a caller writing its own loop wants the same answer.
func IsTerminal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, ErrUnretryable)
}

// Policy executes operations with retry logic.
type Policy interface {
	Execute(ctx context.Context, operation func(ctx context.Context) error) error
}
