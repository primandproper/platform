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

		test.EqOp(t, DefaultTolerance, cfg.Tolerance)
		test.True(t, cfg.At.IsZero())
		test.Nil(t, cfg.Clock)

		// No clock and no pinned time still resolves to something usable.
		test.False(t, cfg.Now().IsZero())
	})

	// A nil option in the variadic list is skipped rather than dereferenced, so
	// a caller building an option slice conditionally cannot panic.
	T.Run("a nil option is skipped", func(t *testing.T) {
		t.Parallel()

		var absent Option

		cfg := newConfig([]Option{absent, WithTolerance(time.Minute)})
		test.EqOp(t, time.Minute, cfg.Tolerance)
	})

	T.Run("WithTolerance", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{WithTolerance(time.Hour)})
		test.EqOp(t, time.Hour, cfg.Tolerance)

		// Non-positive must not disable the freshness check.
		WithTolerance(0)(cfg)
		test.EqOp(t, time.Hour, cfg.Tolerance)

		WithTolerance(-time.Hour)(cfg)
		test.EqOp(t, time.Hour, cfg.Tolerance)
	})

	T.Run("WithVerificationTime", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{WithVerificationTime(signingTime)})
		test.EqOp(t, signingTime, cfg.Now())

		// A zero time is ignored, so this cannot pin verification to the epoch
		// and reject everything.
		WithVerificationTime(time.Time{})(cfg)
		test.EqOp(t, signingTime, cfg.Now())
	})

	T.Run("WithClock", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{WithClock(fixedClock(signingTime))})
		test.EqOp(t, signingTime, cfg.Now())

		WithClock(nil)(cfg)
		must.NotNil(t, cfg.Clock)
	})

	// Both together: the pinned instant wins, so a test or a replay is not at
	// the mercy of whatever clock the component was wired with.
	T.Run("a pinned time beats a clock", func(t *testing.T) {
		t.Parallel()

		cfg := newConfig([]Option{
			WithClock(fixedClock(signingTime.Add(time.Hour))),
			WithVerificationTime(signingTime),
		})

		test.EqOp(t, signingTime, cfg.Now())
	})
}
