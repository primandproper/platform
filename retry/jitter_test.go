package retry

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestFull(T *testing.T) {
	T.Parallel()

	T.Run("draws across the whole interval", func(t *testing.T) {
		t.Parallel()

		j := Full(nil)

		for range 1000 {
			got := j(time.Second)

			must.GreaterEq(t, time.Duration(0), got)
			must.Less(t, time.Second, got)
		}
	})

	// The point of Full over Equal: it is allowed to land near zero, which is
	// what lets a fleet spread across the entire window rather than half of it.
	T.Run("reaches the bottom of the interval", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Duration(0), Full(func() float64 { return 0 })(time.Hour))
	})

	T.Run("a non-positive delay is returned unchanged", func(t *testing.T) {
		t.Parallel()

		j := Full(nil)

		test.EqOp(t, time.Duration(0), j(0))
		test.EqOp(t, -time.Second, j(-time.Second))
	})
}

func TestEqual(T *testing.T) {
	T.Parallel()

	T.Run("keeps half the delay and spreads the rest", func(t *testing.T) {
		t.Parallel()

		j := Equal(nil)

		for range 1000 {
			got := j(time.Second)

			must.GreaterEq(t, time.Second/2, got)
			must.Less(t, time.Second, got)
		}
	})

	// Load-bearing where the un-jittered interval is itself a promise: a secret
	// refresh scheduled inside its TTL, a lock renewal inside its lease.
	T.Run("never exceeds the delay", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, time.Hour, Equal(func() float64 { return 1 })(time.Hour))
	})

	T.Run("a delay too small to halve is returned unchanged", func(t *testing.T) {
		t.Parallel()

		j := Equal(func() float64 { return 0 })

		// Perturbing these to zero would buy nothing and break the floor the
		// strategy is named for.
		test.EqOp(t, time.Duration(1), j(1))
		test.EqOp(t, time.Duration(0), j(0))
		test.EqOp(t, -time.Second, j(-time.Second))
	})
}

func TestNone(T *testing.T) {
	T.Parallel()

	T.Run("returns the delay unchanged", func(t *testing.T) {
		t.Parallel()

		var j Jitter = None

		test.EqOp(t, time.Second, j(time.Second))
		test.EqOp(t, time.Duration(0), j(0))
	})
}

func TestJitter_AtLeast(T *testing.T) {
	T.Parallel()

	T.Run("floors a draw that landed under the minimum", func(t *testing.T) {
		t.Parallel()

		j := Full(func() float64 { return 0 }).AtLeast(time.Millisecond)

		test.EqOp(t, time.Millisecond, j(time.Hour))
	})

	T.Run("leaves a draw above the minimum alone", func(t *testing.T) {
		t.Parallel()

		j := Full(func() float64 { return 1 }).AtLeast(time.Millisecond)

		test.EqOp(t, time.Hour, j(time.Hour))
	})
}

func TestDefaultRand(T *testing.T) {
	T.Parallel()

	T.Run("draws within the unit interval", func(t *testing.T) {
		t.Parallel()

		for range 1000 {
			got := DefaultRand()

			must.GreaterEq(t, 0.0, got)
			must.Less(t, 1.0, got)
		}
	})

	// A strategy built from a zero Rand has to work rather than panic on first
	// use, because the zero value is what an unset option leaves behind.
	T.Run("stands in for an absent source", func(t *testing.T) {
		t.Parallel()

		must.NotEq(t, time.Duration(0), Equal(nil)(time.Hour))
	})
}
