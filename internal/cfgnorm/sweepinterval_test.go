package cfgnorm

import (
	"testing"
	"time"

	"github.com/shoenig/test"
)

func TestEnsureSweepInterval(T *testing.T) {
	T.Parallel()

	const fallback = 5 * time.Minute

	cases := []struct {
		name  string
		input time.Duration
		want  time.Duration
	}{
		{name: "an unset field takes the fallback", input: 0, want: fallback},
		{name: "a configured cadence is kept", input: time.Hour, want: time.Hour},
		// The whole point. A defaulting step that mapped every non-positive
		// value to the fallback is what put the off-switch out of reach.
		{name: "NoSweep survives defaulting", input: NoSweep, want: NoSweep},
	}

	for _, tc := range cases {
		T.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.want, EnsureSweepInterval(tc.input, fallback))
		})
	}
}

func TestSweepIntervalRule(T *testing.T) {
	T.Parallel()

	T.Run("permits a cadence", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate(time.Hour))
	})

	// Zero is an unset field, which EnsureSweepInterval is what answers. The
	// rule saying so too would reject every config that named no interval.
	T.Run("permits an unset field", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate(time.Duration(0)))
	})

	T.Run("permits NoSweep by rule rather than by omission", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate(NoSweep))
	})

	// Below zero there is no cadence to configure — every negative duration
	// reaches a store as "start nothing" — so a magnitude is somebody
	// describing a sweep they will not get.
	T.Run("refuses a negative interval that is not NoSweep", func(t *testing.T) {
		t.Parallel()

		test.Error(t, SweepIntervalRule.Validate(-30*time.Minute))
	})

	// ozzo hands a rule whatever the field holds. A rule that panicked on a
	// surprise would take down a composition root over a config it could
	// simply have had nothing to say about.
	T.Run("has nothing to say about a value that is not a duration", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate("not a duration"))
	})
}
