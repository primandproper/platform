package retry

import (
	"math/rand/v2"
	"time"
)

// Rand draws a value in [0, 1). math/rand/v2's Float64 satisfies it and is what
// DefaultRand is.
//
// It is a parameter rather than a package-level draw so that a caller who needs
// a schedule to be reproducible — a test asserting an exact wait, a simulation
// replaying a fleet — can supply one, and so that nothing here reaches for a
// global source a caller cannot see.
type Rand func() float64

// DefaultRand is the source a strategy uses when none is given: math/rand/v2's
// global Float64, which needs no seeding.
//
// It is not cryptographic and does not need to be. Jitter decorrelates the
// timing of retries between processes; nothing about it is a secret, and a
// caller who could predict the next wait gains nothing by it.
func DefaultRand() float64 { return rand.Float64() } //nolint:gosec // G404: spreading retries does not require cryptographic randomness

// Jitter perturbs a computed backoff so that callers which failed together do
// not retry together.
//
// The strategies below differ in how much of the delay they are willing to give
// up, and that difference is the whole decision: it trades how well a fleet
// spreads against how short a single wait may become. Naming them is what keeps
// the choice visible at the call site — the same perturbation written inline
// reads as arithmetic, and two call sites that meant different distributions
// look identical.
//
// A strategy is expected to be monotone in nothing and to return a
// non-negative duration for a non-negative one; a zero or negative delay comes
// back unchanged, because there is nothing to spread.
type Jitter func(d time.Duration) time.Duration

// Full spreads a delay across the whole interval, drawing uniformly from
// [0, d).
//
// It is the strongest spread available and the right one when many processes
// write their next attempt somewhere durable and then stop thinking about it:
// a fleet that all failed on the same contended row wants its next attempts
// scattered over the entire window, because anything less leaves a shoulder
// they will re-collide on.
//
// The cost is that a single wait can land arbitrarily close to zero, which for
// a caller that sleeps in place is a hot loop. Such callers want Equal, or a
// Full floored by AtLeast.
func Full(r Rand) Jitter {
	r = ensureRand(r)

	return func(d time.Duration) time.Duration {
		if d <= 0 {
			return d
		}

		return time.Duration(float64(d) * r())
	}
}

// Equal holds half the delay and spreads the other half, drawing from
// [d/2, d).
//
// It is the right one when the caller waits in place, because it keeps a floor
// of half the schedule under every wait: a poller that backed off to ten
// seconds cannot draw a ten-millisecond one and turn back into a hot loop. A
// fleet still spreads, just across half the window rather than all of it.
//
// It never exceeds d, which is load-bearing wherever the un-jittered interval
// is itself a promise — a refresh scheduled inside a TTL, a renewal inside a
// lease.
//
// A delay too small to halve comes back unchanged. Perturbing a one-nanosecond
// wait buys nothing and would have to break the floor this strategy is named
// for to do it.
func Equal(r Rand) Jitter {
	r = ensureRand(r)

	return func(d time.Duration) time.Duration {
		half := d / 2
		if half <= 0 {
			return d
		}

		return half + time.Duration(float64(d-half)*r())
	}
}

// None returns the delay unchanged.
//
// It is what a UseJitter=false config selects, so that the presence of jitter
// is a choice of strategy rather than a branch around one.
func None(d time.Duration) time.Duration { return d }

// AtLeast floors a strategy's output.
//
// Full can draw a delay arbitrarily close to zero, and for a caller that
// persists "try again at T" that means a row which becomes claimable
// immediately and spins against whatever failure produced it rather than
// waiting the failure out. The floor is what makes the strongest spread safe
// for a caller that cannot sleep.
func (j Jitter) AtLeast(minimum time.Duration) Jitter {
	return func(d time.Duration) time.Duration {
		return max(j(d), minimum)
	}
}

// ensureRand resolves an absent source to DefaultRand, so that a strategy built
// from a zero value works rather than panicking on first use.
func ensureRand(r Rand) Rand {
	if r == nil {
		return DefaultRand
	}

	return r
}
