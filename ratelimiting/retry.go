package ratelimiting

import (
	"context"
	"time"
)

// RetryHinter is the optional half of RateLimiter: an implementation able to
// say when a refused key will next be allowed implements it, and the callers
// that can pass such a hint on — an HTTP middleware writing Retry-After, a gRPC
// interceptor attaching RetryInfo — ask for it through RetryAfterFor.
//
// It is a second interface rather than a third method on RateLimiter because
// not every limiter can answer it. One fronting a third party knows only what
// that party told it, which is often nothing. Widening RateLimiter would force
// those implementations to invent a number, and an invented Retry-After is
// worse than none at all: clients obey it, so a wrong one either wastes the
// capacity that was actually free or sends everyone back at the same instant.
//
// The refusal itself is still expressed by Allow. A hint is an improvement on a
// refusal, never a substitute for one.
type RetryHinter interface {
	// RetryAfter estimates how long key must wait before Allow would say yes
	// again. ok is false when there is no estimate to give — including for a
	// key this limiter has not seen.
	//
	// It is an estimate by construction: nothing reserves the capacity it
	// describes, so another caller may spend it first. Treat it as a floor
	// under how long to wait, not as a promise about what happens after.
	RetryAfter(ctx context.Context, key string) (time.Duration, bool)
}

// RetryAfterFor asks limiter when key will next be allowed, returning
// (0, false) when it cannot say — either because it does not implement
// RetryHinter or because it has no estimate for this key.
//
// It exists so that the transport adapters share one type assertion instead of
// each writing their own, which is what keeps an unhinted refusal behave
// identically over HTTP and over gRPC.
//
// A negative duration is reported as no hint rather than passed along: it would
// mean the key is already allowed, and telling a client to come back in the
// past is the same as telling it nothing.
func RetryAfterFor(ctx context.Context, limiter RateLimiter, key string) (time.Duration, bool) {
	hinter, ok := limiter.(RetryHinter)
	if !ok {
		return 0, false
	}

	delay, ok := hinter.RetryAfter(ctx, key)
	if !ok || delay < 0 {
		return 0, false
	}

	return delay, true
}
