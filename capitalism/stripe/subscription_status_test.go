package stripe

import (
	"testing"

	"github.com/primandproper/platform-go/v14/capitalism"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/stripe/stripe-go/v81"
)

func TestMapSubscriptionStatus(T *testing.T) {
	T.Parallel()

	// Every subscription status Stripe documents, including the ones the webhook path's
	// old switch never looked at. The list is spelled out rather than derived from the
	// mapping table, so a status dropped from the table fails here instead of quietly
	// agreeing with itself.
	documented := map[stripe.SubscriptionStatus]capitalism.SubscriptionStatus{
		stripe.SubscriptionStatusIncomplete:        capitalism.SubscriptionStatusIncomplete,
		stripe.SubscriptionStatusIncompleteExpired: capitalism.SubscriptionStatusIncompleteExpired,
		stripe.SubscriptionStatusTrialing:          capitalism.SubscriptionStatusTrialing,
		stripe.SubscriptionStatusActive:            capitalism.SubscriptionStatusActive,
		stripe.SubscriptionStatusPastDue:           capitalism.SubscriptionStatusPastDue,
		stripe.SubscriptionStatusCanceled:          capitalism.SubscriptionStatusCanceled,
		stripe.SubscriptionStatusUnpaid:            capitalism.SubscriptionStatusUnpaid,
		stripe.SubscriptionStatusPaused:            capitalism.SubscriptionStatusPaused,
	}

	T.Run("maps every status Stripe documents", func(t *testing.T) {
		t.Parallel()

		for reported, want := range documented {
			got, ok := MapSubscriptionStatus(string(reported))

			test.True(t, ok, test.Sprintf("status %q", reported))
			test.EqOp(t, want, got, test.Sprintf("status %q", reported))
			test.True(t, got.Known(), test.Sprintf("status %q", reported))
		}
	})

	T.Run("covers the documented set and nothing else", func(t *testing.T) {
		t.Parallel()

		// An entry in the table with no counterpart above is a status this package
		// invented, and a counterpart with no entry is one it forgot.
		must.MapLen(t, len(documented), subscriptionStatuses)

		for reported := range subscriptionStatuses {
			_, ok := documented[reported]
			test.True(t, ok, test.Sprintf("status %q", reported))
		}
	})

	T.Run("reports a status it does not recognize as unknown", func(t *testing.T) {
		t.Parallel()

		// The second return is what separates "Stripe says canceled" from "Stripe says
		// something we have never seen", and only the first is grounds for cutting off
		// an account. A status Stripe adds later lands here.
		got, ok := MapSubscriptionStatus("gone_fishing")

		test.False(t, ok)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, got)
		test.False(t, got.Known())
	})

	T.Run("reports an absent status as unknown", func(t *testing.T) {
		t.Parallel()

		// A payload with no status field decodes to the empty string, which is not one
		// of the eight and must not become one.
		got, ok := MapSubscriptionStatus("")

		test.False(t, ok)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, got)
	})
}
