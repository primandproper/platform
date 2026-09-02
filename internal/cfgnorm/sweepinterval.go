package cfgnorm

import (
	"time"

	"github.com/primandproper/platform-go/v14/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// NoSweep is the SweepInterval that starts no sweeper, for a deployment whose
// scheduler calls the store's own Sweep instead — one sweep for the fleet
// rather than one per replica.
//
// It is negative rather than zero because zero is what an unset environment
// variable and an unset struct field both produce, and an EnsureDefaults cannot
// tell an operator who wants no sweeper from one who said nothing. The two
// readings do not cost the same: a sweeper nobody needed is a periodic DELETE
// that finds nothing, while a sweeper nobody started is a table that only
// grows. So zero takes the package's default, and turning the sweeper off is a
// decision that has to be spelled.
//
// Spelled in the environment, that is SWEEP_INTERVAL=-1ns: the value is a
// duration the environment parses like any other, and it is the only negative
// one SweepIntervalRule accepts.
//
// Each config package re-exports this as its own NoSweep, so that a deployment
// configuring sessions says sessionscfg.NoSweep and does not have to know the
// constant is shared. What is shared is the decision, which is the part that
// could be made differently in three places and then be three answers.
const NoSweep = time.Duration(-1)

// EnsureSweepInterval returns interval, or fallback when interval is zero.
//
// Call it from EnsureDefaults, and only where a sweeper is wanted: a provider
// with nothing to sweep — a cache that reclaims its own entries — should leave
// the field alone rather than default it to a cadence it will not run.
//
// It exists so that the one branch NoSweep depends on is written once. The
// off-switch is only reachable while defaulting distinguishes a zero from a
// negative, and an EnsureDefaults that mapped every non-positive value to the
// fallback would put it back out of reach without failing a single test that
// did not think to look.
func EnsureSweepInterval(interval, fallback time.Duration) time.Duration {
	if interval == 0 {
		return fallback
	}

	return interval
}

// SweepIntervalRule permits a non-negative interval and NoSweep, and nothing
// else below zero.
//
// Below zero there is no magnitude to mean anything — every negative duration
// reaches a store as "start nothing" — so a deployment that picked one is
// describing a cadence it will not get. Naming NoSweep is the same decision
// made where a reader of the Config can see it.
//
// Apply it under every provider, including the ones that ignore the field. A
// provider ignoring a value is not a reason to permit nonsense in it, and a
// configuration that later moves to the provider that reads it should not start
// failing validation on a value it has carried all along.
var SweepIntervalRule = validation.By(func(value any) error {
	interval, ok := value.(time.Duration)
	if !ok {
		return nil
	}

	if interval < 0 && interval != NoSweep {
		return errors.New("must be a non-negative duration, or NoSweep to start no sweeper")
	}

	return nil
})
