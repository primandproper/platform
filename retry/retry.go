package retry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
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

// isTerminal reports whether an operation error should abort the retry loop
// rather than trigger another attempt: this loop's own context is done, so
// retrying can never succeed, or the error is explicitly non-retryable.
//
// The question is asked of ctx, not of err. Matching err against
// context.DeadlineExceeded treats a *per-attempt* timeout as terminal — and a
// per-attempt timeout is the single most common transient failure there is, so
// the loop gave up on attempt one for exactly the case it exists to survive.
// An operation that bounds itself with its own context.WithTimeout hands back a
// wrapped DeadlineExceeded while this loop's deadline is nowhere near.
func isTerminal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, ErrUnretryable)
}

// Policy executes operations with retry logic.
type Policy interface {
	Execute(ctx context.Context, operation func(ctx context.Context) error) error
}

// DelayFor returns the backoff before attempt, which is 1-indexed: attempt 1 is
// the first retry and waits InitialDelay. The delay grows by Multiplier per
// attempt and is capped at MaxDelay. An attempt below 1 is treated as 1.
//
// It exists because not every caller can retry by sleeping. Execute owns its
// loop and can wait in place; a worker that persists "try again at T" into a
// row cannot, because the wait has to survive the process. Both want the same
// schedule, and computing it twice is how the two quietly stop agreeing.
//
// Jitter is deliberately not applied here. Sleeping and scheduling want
// different distributions — Execute uses equal jitter to keep a floor under
// each wait, while a fleet writing wake-up times wants full jitter to spread
// them — so the shared part is the schedule and the caller decides how to
// perturb it. Callers pass a Config that has been through EnsureDefaults;
// DelayFor does not mutate its argument.
func DelayFor(cfg Config, attempt uint) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := float64(cfg.InitialDelay) * math.Pow(cfg.Multiplier, float64(attempt-1))

	if maxDelay := float64(cfg.MaxDelay); delay > maxDelay {
		return cfg.MaxDelay
	}

	return time.Duration(delay)
}

// exponentialBackoff implements Policy with configurable exponential backoff.
type exponentialBackoff struct {
	config Config
}

// NewExponentialBackoffPolicy returns a Policy that retries with exponential backoff.
func NewExponentialBackoffPolicy(cfg Config) Policy {
	cfg.EnsureDefaults()
	return &exponentialBackoff{config: cfg}
}

// Execute runs the operation, retrying on failure up to MaxAttempts times.
func (e *exponentialBackoff) Execute(ctx context.Context, operation func(ctx context.Context) error) error {
	var lastErr error

	for attempt := uint(0); attempt < e.config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		default:
		}

		lastErr = operation(ctx)
		if lastErr == nil {
			return nil
		}

		// A canceled/expired loop context or an explicitly non-retryable error can
		// never be resolved by another attempt — return immediately instead of
		// sleeping and burning the remaining attempts.
		if isTerminal(ctx, lastErr) {
			return lastErr
		}

		if attempt == e.config.MaxAttempts-1 {
			return lastErr
		}

		// attempt is 0-indexed here and DelayFor is 1-indexed: the wait after
		// the first failed attempt is the first retry's delay.
		delay := DelayFor(e.config, attempt+1)

		sleepDuration := delay
		// half > 0 guards rand.Int64N, which panics on a non-positive argument
		// (e.g. a sub-2ns delay where int64(delay)/2 truncates to 0). When the
		// delay is too small to halve, jitter is simply skipped.
		if half := delay / 2; e.config.UseJitter && half > 0 {
			jitter := time.Duration(rand.Int64N(int64(half))) //nolint:gosec // G404: jitter does not require cryptographic randomness
			sleepDuration = delay - half + jitter
		}

		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(sleepDuration):
		}
	}

	return lastErr
}
