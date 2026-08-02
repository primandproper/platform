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
