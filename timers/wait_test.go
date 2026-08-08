package timers

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The next-due read is what decides how long Wait sleeps, and it needs a
// server; the containers tests cover that half. These cover the other half —
// what the sleep does once its length has been decided — which is where the
// wake floor and the wakeup race live.

func newSleeper(t *testing.T, mutate func(*Config), wakeup <-chan struct{}) *Timers[string] {
	t.Helper()

	cfg := validConfig()
	if mutate != nil {
		mutate(cfg)
	}

	opts := []Option{}
	if wakeup != nil {
		opts = append(opts, WithWakeup(wakeup))
	}

	set, err := New[string](t.Context(), cfg, postgresClient(), opts...)
	must.NoError(t, err)

	return set
}

func TestTimers_Sleep(T *testing.T) {
	T.Parallel()

	T.Run("parks for the whole duration", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			set := newSleeper(t, nil, nil)

			start := set.Clock().Now()

			must.NoError(t, set.sleep(t.Context(), time.Hour))

			test.EqOp(t, time.Hour, set.Clock().Since(start))
		})
	})

	// The whole point of a wakeup: a timer scheduled for thirty seconds from
	// now, landing just after a poller went to sleep for an hour, must not wait
	// out the hour.
	T.Run("a wakeup cuts the sleep short", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			wakeup := make(chan struct{}, 1)
			set := newSleeper(t, nil, wakeup)

			start := set.Clock().Now()

			done := make(chan error, 1)
			go func() { done <- set.sleep(t.Context(), time.Hour) }()

			// Wait for the sleeper to be durably parked, so the wake is racing
			// the ticker rather than arriving before it exists.
			synctest.Wait()
			wakeup <- struct{}{}

			must.NoError(t, <-done)
			test.EqOp(t, time.Duration(0), set.Clock().Since(start))
		})
	})

	T.Run("a cancelled context ends the sleep", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			set := newSleeper(t, nil, nil)

			ctx, cancel := context.WithCancel(t.Context())

			done := make(chan error, 1)
			go func() { done <- set.sleep(ctx, time.Hour) }()

			synctest.Wait()
			cancel()

			test.ErrorIs(t, <-done, context.Canceled)
		})
	})
}
