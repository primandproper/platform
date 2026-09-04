package cfgnorm

import (
	"time"

	"github.com/primandproper/platform-go/v14/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// EnsureSweepInterval returns interval, or a pointer to fallback when interval
// is nil.
//
// Call it from EnsureDefaults, and only where a sweeper is wanted: a provider
// with nothing to sweep — a cache that reclaims its own entries — should leave
// the field alone rather than default it to a cadence it will not run.
//
// The pointer is what makes the off-switch reachable, and it is the whole
// reason this helper exists. A time.Duration field has one zero and two things
// to say with it, so an EnsureDefaults cannot tell an operator who wants no
// sweeper from one who said nothing — and the two readings do not cost the
// same. A sweeper nobody needed is a periodic DELETE that finds nothing; a
// sweeper nobody started is a table that only grows. A nil pointer is the one
// that said nothing, so it takes the fallback. A zero is a deployment that
// spelled it, and it survives this call to reach the store as no sweeper at
// all, which is what every WithSweeper already reads a non-positive interval to
// mean.
//
// In the environment that distinction is the difference between an unset
// SWEEP_INTERVAL and SWEEP_INTERVAL=0: env leaves a pointer field nil when the
// variable is absent, and only absence takes the default.
func EnsureSweepInterval(interval *time.Duration, fallback time.Duration) *time.Duration {
	if interval == nil {
		return &fallback
	}

	return interval
}

// SweepIntervalRule permits a nil or non-negative interval, and nothing below
// zero.
//
// Below zero there is no magnitude left to mean anything — every negative
// duration reaches a store as "start nothing", which zero already says without
// the ambiguity — so a deployment that picked one is describing a cadence it
// will not get.
//
// Apply it under every provider, including the ones that ignore the field. A
// provider ignoring a value is not a reason to permit nonsense in it, and a
// configuration that later moves to the provider that reads it should not start
// failing validation on a value it has carried all along.
//
// It reads a *time.Duration and a time.Duration alike, because ozzo hands a
// rule the field's own value and only some of the fields it guards are
// pointers.
var SweepIntervalRule = validation.By(func(value any) error {
	var interval time.Duration

	switch typed := value.(type) {
	case nil:
		return nil
	case *time.Duration:
		if typed == nil {
			return nil
		}

		interval = *typed
	case time.Duration:
		interval = typed
	default:
		return nil
	}

	if interval < 0 {
		return errors.New("must be a non-negative duration; zero starts no sweeper")
	}

	return nil
})
