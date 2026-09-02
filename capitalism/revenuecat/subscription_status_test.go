package revenuecat

import (
	"testing"

	"github.com/primandproper/platform-go/v14/capitalism"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// documentedStatuses is every RevenueCat event type that reports a subscription
// standing, spelled out here rather than derived from the mapping table, so that
// an entry dropped from the table fails here instead of quietly agreeing with
// itself.
var documentedStatuses = map[string]capitalism.SubscriptionStatus{
	"INITIAL_PURCHASE":            capitalism.SubscriptionStatusActive,
	"RENEWAL":                     capitalism.SubscriptionStatusActive,
	"UNCANCELLATION":              capitalism.SubscriptionStatusActive,
	"NON_RENEWING_PURCHASE":       capitalism.SubscriptionStatusActive,
	"PRODUCT_CHANGE":              capitalism.SubscriptionStatusActive,
	"SUBSCRIPTION_EXTENDED":       capitalism.SubscriptionStatusActive,
	"REFUND_REVERSED":             capitalism.SubscriptionStatusActive,
	"TEMPORARY_ENTITLEMENT_GRANT": capitalism.SubscriptionStatusActive,
	"CANCELLATION":                capitalism.SubscriptionStatusActive,
	"BILLING_ISSUE":               capitalism.SubscriptionStatusPastDue,
	"SUBSCRIPTION_PAUSED":         capitalism.SubscriptionStatusPaused,
	"EXPIRATION":                  capitalism.SubscriptionStatusCanceled,
}

// documentedSilences is every RevenueCat event type that reports no subscription
// standing, spelled out for the same reason.
var documentedSilences = []string{
	"TEST",
	"TRANSFER",
	"SUBSCRIBER_ALIAS",
	"INVOICE_ISSUANCE",
	"VIRTUAL_CURRENCY_TRANSACTION",
	"EXPERIMENT_ENROLLMENT",
	"PURCHASE_REDEEMED",
	"PRICE_INCREASE_CONSENT_REQUIRED",
	"PRICE_INCREASE_CONSENT_APPROVED",
}

func TestMapSubscriptionStatus(T *testing.T) {
	T.Parallel()

	T.Run("maps every event type that reports a standing", func(t *testing.T) {
		t.Parallel()

		for reported, want := range documentedStatuses {
			got, ok := MapSubscriptionStatus(reported)

			test.True(t, ok, test.Sprintf("event type %q", reported))
			test.EqOp(t, want, got, test.Sprintf("event type %q", reported))
			test.True(t, got.Known(), test.Sprintf("event type %q", reported))
		}
	})

	T.Run("covers the documented set and nothing else", func(t *testing.T) {
		t.Parallel()

		// An entry in the table with no counterpart above is an event type this
		// package invented, and a counterpart with no entry is one it forgot.
		must.MapLen(t, len(documentedStatuses), subscriptionStatuses)

		for reported := range subscriptionStatuses {
			_, ok := documentedStatuses[reported]
			test.True(t, ok, test.Sprintf("event type %q", reported))
		}
	})

	T.Run("lands on capitalism's vocabulary without inventing a value", func(t *testing.T) {
		t.Parallel()

		// The point of a second adapter: RevenueCat's event types fold onto the
		// existing set rather than revealing a missing one.
		for reported, mapped := range subscriptionStatuses {
			test.True(t, mapped.Known(), test.Sprintf("event type %q", reported))
		}
	})

	T.Run("reports the events that carry no standing as no standing", func(t *testing.T) {
		t.Parallel()

		for _, reported := range documentedSilences {
			got, ok := MapSubscriptionStatus(reported)

			test.False(t, ok, test.Sprintf("event type %q", reported))
			test.EqOp(t, capitalism.SubscriptionStatusUnknown, got, test.Sprintf("event type %q", reported))
		}
	})

	T.Run("knows which silence is which", func(t *testing.T) {
		t.Parallel()

		// The two sets are the whole documented roster and they do not overlap: an
		// event type in both would be one whose standing is reported and ignored.
		must.MapLen(t, len(documentedSilences), eventsWithoutSubscription)

		for _, reported := range documentedSilences {
			_, absent := eventsWithoutSubscription[reported]
			test.True(t, absent, test.Sprintf("event type %q", reported))

			_, mapped := subscriptionStatuses[reported]
			test.False(t, mapped, test.Sprintf("event type %q", reported))
		}
	})

	T.Run("reports an event type it does not recognize as unknown", func(t *testing.T) {
		t.Parallel()

		// The second return is what separates "RevenueCat says this expired" from
		// "RevenueCat says something we have never seen", and only the first is
		// grounds for cutting off an account. An event type RevenueCat adds later
		// lands here.
		got, ok := MapSubscriptionStatus("GONE_FISHING")

		test.False(t, ok)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, got)
		test.False(t, got.Known())
	})

	T.Run("reports an absent event type as unknown", func(t *testing.T) {
		t.Parallel()

		// A payload with no type field decodes to the empty string, which is not one
		// of the documented types and must not become one.
		got, ok := MapSubscriptionStatus("")

		test.False(t, ok)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, got)
	})

	T.Run("does not accept the provider's words in another case", func(t *testing.T) {
		t.Parallel()

		// RevenueCat spells its event types in upper snake case and this table is
		// keyed on exactly that. A lowercase spelling is not a delivery RevenueCat
		// sent, and guessing at it would be the adapter deciding what the provider
		// meant.
		got, ok := MapSubscriptionStatus("renewal")

		test.False(t, ok)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, got)
	})
}

func TestSubscriptionStatus_folds(T *testing.T) {
	T.Parallel()

	T.Run("folds a trial purchase onto trialing", func(t *testing.T) {
		t.Parallel()

		for _, eventType := range []string{EventTypeInitialPurchase, EventTypeRenewal, EventTypeProductChange} {
			got, known := subscriptionStatus(&webhookEvent{Type: eventType, PeriodType: PeriodTypeTrial})

			test.True(t, known, test.Sprintf("event type %q", eventType))
			test.EqOp(t, capitalism.SubscriptionStatusTrialing, got, test.Sprintf("event type %q", eventType))
		}
	})

	T.Run("leaves every other period type paid up", func(t *testing.T) {
		t.Parallel()

		// An introductory price, a promotional grant and a prepaid plan are all a
		// subscriber who is entitled, which is what Active says. Only a trial is a
		// standing of its own.
		for _, periodType := range []string{PeriodTypeIntro, PeriodTypeNormal, PeriodTypePromotional, PeriodTypePrepaid, ""} {
			got, known := subscriptionStatus(&webhookEvent{Type: EventTypeInitialPurchase, PeriodType: periodType})

			test.True(t, known, test.Sprintf("period type %q", periodType))
			test.EqOp(t, capitalism.SubscriptionStatusActive, got, test.Sprintf("period type %q", periodType))
		}
	})

	T.Run("does not fold a trial period onto a standing that is not active", func(t *testing.T) {
		t.Parallel()

		// A billing issue during a trial is still a billing issue. Trialing would
		// say the store is happily not charging anyone, which is the opposite.
		got, known := subscriptionStatus(&webhookEvent{Type: EventTypeBillingIssue, PeriodType: PeriodTypeTrial})

		test.True(t, known)
		test.EqOp(t, capitalism.SubscriptionStatusPastDue, got)
	})

	T.Run("keeps a cancelled subscriber entitled until it expires", func(t *testing.T) {
		t.Parallel()

		// RevenueCat reports a cancellation when auto-renew goes off. The subscriber
		// keeps the period they paid for, and EXPIRATION is what says otherwise.
		for _, reason := range []string{CancelReasonUnsubscribe, CancelReasonPriceIncrease, CancelReasonUnknown, "SOMETHING_NEW", ""} {
			got, known := subscriptionStatus(&webhookEvent{Type: EventTypeCancellation, CancelReason: reason})

			test.True(t, known, test.Sprintf("cancel reason %q", reason))
			test.EqOp(t, capitalism.SubscriptionStatusActive, got, test.Sprintf("cancel reason %q", reason))
		}
	})

	T.Run("ends access for the cancellations that end access", func(t *testing.T) {
		t.Parallel()

		for reason := range cancellationsThatEndAccess {
			got, known := subscriptionStatus(&webhookEvent{Type: EventTypeCancellation, CancelReason: reason})

			test.True(t, known, test.Sprintf("cancel reason %q", reason))
			test.EqOp(t, capitalism.SubscriptionStatusCanceled, got, test.Sprintf("cancel reason %q", reason))
		}
	})

	T.Run("names only the reasons that end access", func(t *testing.T) {
		t.Parallel()

		// Spelled out separately from the set, so a reason added to it without a
		// reason to be there fails here.
		test.Eq(t, map[string]struct{}{
			"BILLING_ERROR":       {},
			"DEVELOPER_INITIATED": {},
			"CUSTOMER_SUPPORT":    {},
		}, cancellationsThatEndAccess)
	})

	T.Run("does not fold a cancel reason onto an event that is not a cancellation", func(t *testing.T) {
		t.Parallel()

		// RevenueCat repeats cancel_reason on the EXPIRATION that follows a
		// cancellation, and on a refund's reversal. Reading it anywhere but a
		// CANCELLATION would let a stale field move a standing the type already
		// settled.
		got, known := subscriptionStatus(&webhookEvent{Type: EventTypeRefundReversed, CancelReason: CancelReasonCustomerSupport})

		test.True(t, known)
		test.EqOp(t, capitalism.SubscriptionStatusActive, got)
	})

	T.Run("reports an unrecognized event type without folding anything onto it", func(t *testing.T) {
		t.Parallel()

		got, known := subscriptionStatus(&webhookEvent{Type: "GONE_FISHING", PeriodType: PeriodTypeTrial, CancelReason: CancelReasonCustomerSupport})

		test.False(t, known)
		test.EqOp(t, capitalism.SubscriptionStatusUnknown, got)
	})
}
