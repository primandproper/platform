package stripe

import (
	"github.com/primandproper/platform-go/v14/capitalism"

	"github.com/stripe/stripe-go/v81"
)

// subscriptionStatuses maps every subscription status Stripe documents onto
// capitalism's vocabulary.
//
// It is a table rather than a switch, and it is here rather than in every
// consumer, because this is the one file in the module that should know what
// "past_due" means. A consumer writing that switch itself writes it against
// whichever three statuses it had seen in testing, and nothing tells it the set
// has eight members — an omission that surfaces as an account left entitled
// after it lapsed rather than as a compile error.
//
// The mapping is currently one-to-one, which is not the same as being
// unnecessary: it is what keeps stripe-go's constants out of the exported API,
// and what lets a second adapter whose provider says "deleted" or "grace" land
// on the same eight values.
var subscriptionStatuses = map[stripe.SubscriptionStatus]capitalism.SubscriptionStatus{
	stripe.SubscriptionStatusIncomplete:        capitalism.SubscriptionStatusIncomplete,
	stripe.SubscriptionStatusIncompleteExpired: capitalism.SubscriptionStatusIncompleteExpired,
	stripe.SubscriptionStatusTrialing:          capitalism.SubscriptionStatusTrialing,
	stripe.SubscriptionStatusActive:            capitalism.SubscriptionStatusActive,
	stripe.SubscriptionStatusPastDue:           capitalism.SubscriptionStatusPastDue,
	stripe.SubscriptionStatusCanceled:          capitalism.SubscriptionStatusCanceled,
	stripe.SubscriptionStatusUnpaid:            capitalism.SubscriptionStatusUnpaid,
	stripe.SubscriptionStatusPaused:            capitalism.SubscriptionStatusPaused,
}

// MapSubscriptionStatus translates a status as Stripe reported it onto
// capitalism's vocabulary, reporting whether it recognized the status.
//
// It takes a string rather than stripe.SubscriptionStatus so that it stays
// callable from a consumer holding a status it read out of a raw payload,
// without that consumer taking on a stripe-go major of its own.
//
// A status Stripe adds after this module was built maps to
// capitalism.SubscriptionStatusUnknown and reports false, rather than to
// whichever known value a default arm happened to name. That is the whole point
// of the second return: the caller can tell "Stripe says this subscription is
// canceled" from "Stripe says something we have never seen", and only the first
// is grounds for cutting off an account.
func MapSubscriptionStatus(status string) (capitalism.SubscriptionStatus, bool) {
	mapped, ok := subscriptionStatuses[stripe.SubscriptionStatus(status)]
	if !ok {
		return capitalism.SubscriptionStatusUnknown, false
	}

	return mapped, true
}
