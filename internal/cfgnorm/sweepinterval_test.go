package cfgnorm

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestEnsureSweepInterval(T *testing.T) {
	T.Parallel()

	const fallback = 5 * time.Minute

	cases := []struct {
		input *time.Duration
		name  string
		want  time.Duration
	}{
		{name: "an unset field takes the fallback", input: nil, want: fallback},
		{name: "a configured cadence is kept", input: pointer.To(time.Hour), want: time.Hour},
		// The whole point. A defaulting step that could not tell a zero from an
		// unset field is what put the off-switch out of reach.
		{name: "a spelled zero survives defaulting", input: pointer.To(time.Duration(0)), want: 0},
	}

	for _, tc := range cases {
		T.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := EnsureSweepInterval(tc.input, fallback)
			must.NotNil(t, got)
			test.EqOp(t, tc.want, *got)
		})
	}
}

// A caller that hands its own pointer in gets that pointer back, rather than a
// copy — the fallback is the only value this function has to allocate for.
func TestEnsureSweepIntervalKeepsTheCallersPointer(t *testing.T) {
	t.Parallel()

	interval := pointer.To(time.Hour)

	test.EqOp(t, interval, EnsureSweepInterval(interval, 5*time.Minute))
}

func TestSweepIntervalRule(T *testing.T) {
	T.Parallel()

	T.Run("permits a cadence", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate(pointer.To(time.Hour)))
	})

	// Nil is the unset field, which EnsureSweepInterval is what answers. The
	// rule saying so too would reject every config that named no interval.
	T.Run("permits an unset field", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate((*time.Duration)(nil)))
		test.NoError(t, SweepIntervalRule.Validate(nil))
	})

	T.Run("permits a spelled zero, which is the off-switch", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate(pointer.To(time.Duration(0))))
	})

	// Below zero there is no cadence to configure — every negative duration
	// reaches a store as "start nothing", which zero already says — so a
	// magnitude is somebody describing a sweep they will not get.
	T.Run("refuses a negative interval", func(t *testing.T) {
		t.Parallel()

		test.Error(t, SweepIntervalRule.Validate(pointer.To(-30*time.Minute)))
	})

	// ozzo hands a rule the field's own value, and not every field this rule
	// guards has to be a pointer for it to keep working.
	T.Run("reads a bare duration too", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, SweepIntervalRule.Validate(time.Hour))
		test.NoError(t, SweepIntervalRule.Validate(time.Duration(0)))
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
