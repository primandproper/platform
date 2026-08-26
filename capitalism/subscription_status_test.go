package capitalism

import (
	"testing"

	"github.com/shoenig/test"
)

func TestSubscriptionStatus_Known(T *testing.T) {
	T.Parallel()

	T.Run("recognizes every status this module names", func(t *testing.T) {
		t.Parallel()

		for _, status := range []SubscriptionStatus{
			SubscriptionStatusIncomplete,
			SubscriptionStatusIncompleteExpired,
			SubscriptionStatusTrialing,
			SubscriptionStatusActive,
			SubscriptionStatusPastDue,
			SubscriptionStatusCanceled,
			SubscriptionStatusUnpaid,
			SubscriptionStatusPaused,
		} {
			test.True(t, status.Known(), test.Sprintf("status %q", status))
		}
	})

	T.Run("does not recognize the unknown status", func(t *testing.T) {
		t.Parallel()

		// The zero value is the unknown status, so a hand-built SubscriptionState that
		// nobody filled in reports "we do not know" rather than one of the eight.
		test.False(t, SubscriptionStatusUnknown.Known())
		test.False(t, SubscriptionStatus("").Known())
	})

	T.Run("does not recognize a status invented elsewhere", func(t *testing.T) {
		t.Parallel()

		test.False(t, SubscriptionStatus("gone_fishing").Known())
		// Near-misses too: a provider that says "cancelled" has not said "canceled", and
		// an adapter is what reconciles the spelling.
		test.False(t, SubscriptionStatus("cancelled").Known())
		test.False(t, SubscriptionStatus("ACTIVE").Known())
	})
}

func TestSubscriptionStatus_String(T *testing.T) {
	T.Parallel()

	T.Run("renders a known status as it is spelled", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "past_due", SubscriptionStatusPastDue.String())
		test.EqOp(t, "incomplete_expired", SubscriptionStatusIncompleteExpired.String())
	})

	T.Run("renders the unknown status as a word rather than a blank", func(t *testing.T) {
		t.Parallel()

		// A log line or span attribute that went empty here would read as a missing
		// field rather than as the case it is.
		test.EqOp(t, "unknown", SubscriptionStatusUnknown.String())
	})
}
