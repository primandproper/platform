package sessions

import (
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var testEpoch = time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

func TestPolicy_Validate(T *testing.T) {
	T.Parallel()

	T.Run("accepts two timeouts and a shorter touch interval", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, Policy{Absolute: time.Hour, Idle: 30 * time.Minute, Touch: time.Minute}.Validate())
	})

	T.Run("accepts either timeout alone", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, Policy{Absolute: time.Hour}.Validate())
		must.NoError(t, Policy{Idle: time.Hour, Touch: time.Minute}.Validate())
	})

	// The combination that would leave a store growing forever, and the reason
	// a Policy is validated rather than merely assembled.
	T.Run("rejects a policy with no timeout at all", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, Policy{}.Validate(), ErrNoTimeout)
		test.ErrorIs(t, Policy{Absolute: -time.Hour, Idle: -time.Hour}.Validate(), ErrNoTimeout)
	})

	T.Run("rejects a negative touch interval", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t,
			Policy{Absolute: time.Hour, Idle: time.Hour, Touch: -time.Second}.Validate(),
			ErrNegativeTouchInterval)
	})

	// A touch interval at least as long as the idle window would let a session
	// expire between the reads that were meant to keep it alive.
	T.Run("rejects a touch interval that does not fit inside the idle window", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t,
			Policy{Absolute: time.Hour, Idle: time.Minute, Touch: time.Minute}.Validate(),
			ErrTouchExceedsIdleTimeout)
		test.ErrorIs(t,
			Policy{Absolute: time.Hour, Idle: time.Minute, Touch: 2 * time.Minute}.Validate(),
			ErrTouchExceedsIdleTimeout)
	})

	// With no idle deadline there is nothing for a touch to refresh, so the
	// interval cannot be wrong.
	T.Run("ignores the touch interval when the idle timeout is disabled", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, Policy{Absolute: time.Hour, Touch: 24 * time.Hour}.Validate())
	})
}

func TestPolicy_Deadline(T *testing.T) {
	T.Parallel()

	T.Run("takes the earlier of the two", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: 2 * time.Hour, Idle: 30 * time.Minute}

		// Freshly seen: the idle deadline is nearer.
		test.EqOp(t, testEpoch.Add(30*time.Minute), p.Deadline(testEpoch, testEpoch))

		// Seen 1h50m in: the absolute deadline is now the nearer one, and the
		// session ends on schedule rather than 30 minutes after its last read.
		lastSeen := testEpoch.Add(110 * time.Minute)
		test.EqOp(t, testEpoch.Add(2*time.Hour), p.Deadline(testEpoch, lastSeen))
	})

	T.Run("uses the only enabled deadline", func(t *testing.T) {
		t.Parallel()

		absoluteOnly := Policy{Absolute: time.Hour}
		test.EqOp(t, testEpoch.Add(time.Hour), absoluteOnly.Deadline(testEpoch, testEpoch))

		idleOnly := Policy{Idle: time.Hour}
		lastSeen := testEpoch.Add(time.Minute)
		test.EqOp(t, lastSeen.Add(time.Hour), idleOnly.Deadline(testEpoch, lastSeen))
	})
}

func TestPolicy_Expiry(T *testing.T) {
	T.Parallel()

	p := Policy{Absolute: 2 * time.Hour, Idle: 30 * time.Minute}

	T.Run("live inside both windows", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ExpiryNone, p.Expiry(testEpoch, testEpoch, testEpoch.Add(time.Minute)))
	})

	T.Run("idle when the last read is too old", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ExpiryIdle, p.Expiry(testEpoch, testEpoch, testEpoch.Add(30*time.Minute)))
	})

	T.Run("absolute when the session is too old", func(t *testing.T) {
		t.Parallel()

		lastSeen := testEpoch.Add(119 * time.Minute)
		test.EqOp(t, ExpiryAbsolute, p.Expiry(testEpoch, lastSeen, testEpoch.Add(2*time.Hour)))
	})

	// Past both, the answer is the one nothing could have prevented. Telling a
	// user they were signed out for inactivity when they were signed out on
	// schedule is the worse of the two wrong answers.
	T.Run("absolute wins when both have passed", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, ExpiryAbsolute, p.Expiry(testEpoch, testEpoch, testEpoch.Add(3*time.Hour)))
	})

	T.Run("the deadline instant itself is expired", func(t *testing.T) {
		t.Parallel()

		// Not merely cosmetic: a session that is live *at* its deadline is live
		// for one more read than the timeout says it may be.
		test.EqOp(t, ExpiryIdle, p.Expiry(testEpoch, testEpoch, testEpoch.Add(30*time.Minute)))
	})

	T.Run("a disabled timeout never expires", func(t *testing.T) {
		t.Parallel()

		idleOnly := Policy{Idle: time.Minute}
		test.EqOp(t, ExpiryNone, idleOnly.Expiry(testEpoch, testEpoch.Add(300*time.Hour), testEpoch.Add(300*time.Hour)))
	})
}

func TestExpiry_ErrAndString(T *testing.T) {
	T.Parallel()

	T.Run("each reason names its sentinel", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, ExpiryAbsolute.Err(), ErrAbsoluteTimeout)
		test.ErrorIs(t, ExpiryIdle.Err(), ErrIdleTimeout)
		must.Nil(t, ExpiryNone.Err())
	})

	T.Run("reasons render as the counter's attribute values", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "absolute", ExpiryAbsolute.String())
		test.EqOp(t, "idle", ExpiryIdle.String())
		test.EqOp(t, "none", ExpiryNone.String())
		test.EqOp(t, "unknown", Expiry(200).String())
	})

	T.Run("an unknown reason still reports an expiry", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, Expiry(200).Err(), ErrExpired)
	})
}

func TestPolicy_TTL(T *testing.T) {
	T.Parallel()

	T.Run("is the idle window while the absolute one is further off", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: 2 * time.Hour, Idle: 30 * time.Minute}
		test.EqOp(t, 30*time.Minute, p.TTL(testEpoch, testEpoch))
	})

	// The clipping is what keeps a backend's own expiry from outliving the
	// absolute deadline: a record written five minutes before the end must not
	// be retrievable for thirty.
	T.Run("clips to what is left of the absolute window", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: 2 * time.Hour, Idle: 30 * time.Minute}
		test.EqOp(t, 5*time.Minute, p.TTL(testEpoch, testEpoch.Add(115*time.Minute)))
	})

	T.Run("is zero once the absolute window is spent", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: time.Hour, Idle: 30 * time.Minute}
		test.EqOp(t, time.Duration(0), p.TTL(testEpoch, testEpoch.Add(time.Hour)))
	})

	T.Run("uses whichever window is enabled alone", func(t *testing.T) {
		t.Parallel()

		absoluteOnly := Policy{Absolute: time.Hour}
		test.EqOp(t, 40*time.Minute, absoluteOnly.TTL(testEpoch, testEpoch.Add(20*time.Minute)))

		idleOnly := Policy{Idle: time.Hour}
		test.EqOp(t, time.Hour, idleOnly.TTL(testEpoch, testEpoch.Add(300*time.Hour)))
	})
}

func TestPolicy_RetentionTTL(T *testing.T) {
	T.Parallel()

	// The grace is what keeps an expired record around long enough for the
	// store to say why the session ended. Without it a cache reclaims the
	// record at the deadline and "you idled out" is indistinguishable from "no
	// such session".
	T.Run("outlives the deadline by the grace period", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: 2 * time.Hour, Idle: 30 * time.Minute, Grace: time.Hour}
		test.EqOp(t, 90*time.Minute, p.RetentionTTL(testEpoch, testEpoch))
	})

	T.Run("is the bare deadline with no grace", func(t *testing.T) {
		t.Parallel()

		p := Policy{Idle: 30 * time.Minute}
		test.EqOp(t, 30*time.Minute, p.RetentionTTL(testEpoch, testEpoch))
	})

	// Nothing is retained for a session that is already over; the store refuses
	// it before it would ever be written.
	T.Run("is zero for a session already past its absolute deadline", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: time.Hour, Idle: 30 * time.Minute, Grace: time.Hour}
		test.EqOp(t, time.Duration(0), p.RetentionTTL(testEpoch, testEpoch.Add(time.Hour)))
	})
}

func TestPolicy_ShouldTouch(T *testing.T) {
	T.Parallel()

	T.Run("only once the interval has elapsed", func(t *testing.T) {
		t.Parallel()

		p := Policy{Idle: 30 * time.Minute, Touch: time.Minute}

		test.False(t, p.ShouldTouch(testEpoch, testEpoch.Add(59*time.Second)))
		test.True(t, p.ShouldTouch(testEpoch, testEpoch.Add(time.Minute)))
	})

	T.Run("every read when the interval is zero", func(t *testing.T) {
		t.Parallel()

		p := Policy{Idle: 30 * time.Minute}
		test.True(t, p.ShouldTouch(testEpoch, testEpoch))
	})

	// Nothing to refresh, so nothing is written — the point being that an
	// absolute-only store costs no writes on the read path at all.
	T.Run("never when the idle timeout is disabled", func(t *testing.T) {
		t.Parallel()

		p := Policy{Absolute: time.Hour}
		test.False(t, p.ShouldTouch(testEpoch, testEpoch.Add(300*time.Hour)))
	})
}
