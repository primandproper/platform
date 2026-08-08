package requestsigning

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestOptions(T *testing.T) {
	T.Parallel()

	T.Run("defaults", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig(nil)

		test.EqOp(t, DefaultTolerance, cfg.tolerance)
		test.True(t, cfg.at.IsZero())
		test.Nil(t, cfg.clock)

		// No clock and no pinned time still resolves to something usable.
		test.False(t, cfg.now().IsZero())
	})

	// A nil option in the variadic list is skipped rather than dereferenced, so
	// a caller building an option slice conditionally cannot panic.
	T.Run("a nil option is skipped", func(t *testing.T) {
		t.Parallel()

		var absent Option

		cfg := newConfig([]Option{absent, WithTolerance(time.Minute)})
		test.EqOp(t, time.Minute, cfg.tolerance)
	})

	T.Run("WithTolerance", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{WithTolerance(time.Hour)})
		test.EqOp(t, time.Hour, cfg.tolerance)

		// Non-positive must not disable the freshness check.
		WithTolerance(0)(cfg)
		test.EqOp(t, time.Hour, cfg.tolerance)

		WithTolerance(-time.Hour)(cfg)
		test.EqOp(t, time.Hour, cfg.tolerance)
	})

	T.Run("WithVerificationTime", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{WithVerificationTime(signingTime)})
		test.EqOp(t, signingTime, cfg.now())

		// A zero time is ignored, so this cannot pin verification to the epoch
		// and reject everything.
		WithVerificationTime(time.Time{})(cfg)
		test.EqOp(t, signingTime, cfg.now())
	})

	T.Run("WithClock", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{WithClock(fixedClock(signingTime))})
		test.EqOp(t, signingTime, cfg.now())

		WithClock(nil)(cfg)
		must.NotNil(t, cfg.clock)
	})

	// Both together: the pinned instant wins, so a test or a replay is not at
	// the mercy of whatever clock the component was wired with.
	T.Run("a pinned time beats a clock", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{
			WithClock(fixedClock(signingTime.Add(time.Hour))),
			WithVerificationTime(signingTime),
		})

		test.EqOp(t, signingTime, cfg.now())
	})
}
