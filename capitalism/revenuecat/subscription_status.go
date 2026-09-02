package revenuecat

import (
	"github.com/primandproper/platform-go/v14/capitalism"
)

// The event types RevenueCat documents.
//
// They are spelled out here because there is no RevenueCat SDK to take them
// from — the payloads decode with the standard library, which is the same
// reasoning that keeps stripe-go out of capitalism proper — and because
// capitalism.Event.Type hands a consumer the provider's own words to switch on.
// Exporting them is what keeps that switch free of string literals nobody
// checks the spelling of.
const (
	// EventTypeTest is RevenueCat's "send a test event" button.
	EventTypeTest = "TEST"
	// EventTypeInitialPurchase is a new subscription.
	EventTypeInitialPurchase = "INITIAL_PURCHASE"
	// EventTypeRenewal is a subscription renewing, or a lapsed subscriber
	// resubscribing.
	EventTypeRenewal = "RENEWAL"
	// EventTypeCancellation is auto-renew being switched off, or a refund. It is
	// not the end of access; see cancellationsThatEndAccess.
	EventTypeCancellation = "CANCELLATION"
	// EventTypeUncancellation is a cancelled but unexpired subscription being
	// re-enabled.
	EventTypeUncancellation = "UNCANCELLATION"
	// EventTypeNonRenewingPurchase is a purchase that will not auto-renew.
	EventTypeNonRenewingPurchase = "NON_RENEWING_PURCHASE"
	// EventTypeSubscriptionPaused is a subscription scheduled to pause at the
	// end of the current period.
	EventTypeSubscriptionPaused = "SUBSCRIPTION_PAUSED"
	// EventTypeExpiration is access ending. It is the event that means "revoke".
	EventTypeExpiration = "EXPIRATION"
	// EventTypeBillingIssue is a failed charge attempt.
	EventTypeBillingIssue = "BILLING_ISSUE"
	// EventTypeProductChange is a subscriber moving to a different product.
	EventTypeProductChange = "PRODUCT_CHANGE"
	// EventTypeSubscriptionExtended is a subscription's term being extended.
	EventTypeSubscriptionExtended = "SUBSCRIPTION_EXTENDED"
	// EventTypeRefundReversed is a refund being reversed, restoring access.
	EventTypeRefundReversed = "REFUND_REVERSED"
	// EventTypeTemporaryEntitlementGrant is access granted while RevenueCat
	// cannot reach the store, so an outage at Apple or Google does not read as a
	// lapsed subscription.
	EventTypeTemporaryEntitlementGrant = "TEMPORARY_ENTITLEMENT_GRANT"
	// EventTypeInvoiceIssuance is a new unpaid invoice.
	EventTypeInvoiceIssuance = "INVOICE_ISSUANCE"
	// EventTypeTransfer is transactions moving between app user IDs.
	EventTypeTransfer = "TRANSFER"
	// EventTypeSubscriberAlias is RevenueCat's deprecated app-user-ID
	// registration event.
	EventTypeSubscriberAlias = "SUBSCRIBER_ALIAS"
	// EventTypeVirtualCurrencyTransaction is a virtual currency balance moving.
	EventTypeVirtualCurrencyTransaction = "VIRTUAL_CURRENCY_TRANSACTION"
	// EventTypeExperimentEnrollment is a customer entering an experiment.
	EventTypeExperimentEnrollment = "EXPERIMENT_ENROLLMENT"
	// EventTypePurchaseRedeemed is a web purchase being redeemed.
	EventTypePurchaseRedeemed = "PURCHASE_REDEEMED"
	// EventTypePriceIncreaseConsentRequired is a price increase awaiting the
	// subscriber's consent.
	EventTypePriceIncreaseConsentRequired = "PRICE_INCREASE_CONSENT_REQUIRED"
	// EventTypePriceIncreaseConsentApproved is a subscriber consenting to a
	// pending price increase.
	EventTypePriceIncreaseConsentApproved = "PRICE_INCREASE_CONSENT_APPROVED"
)

// The period types RevenueCat reports on a purchase or renewal.
const (
	// PeriodTypeTrial is a free trial, which the store has charged nothing for.
	PeriodTypeTrial = "TRIAL"
	// PeriodTypeIntro is a discounted introductory period, which is paid.
	PeriodTypeIntro = "INTRO"
	// PeriodTypeNormal is the standard paid period.
	PeriodTypeNormal = "NORMAL"
	// PeriodTypePromotional is a developer-granted free period. It is not a
	// trial: nothing about it is pending conversion, and the subscriber is
	// entitled.
	PeriodTypePromotional = "PROMOTIONAL"
	// PeriodTypePrepaid is a prepaid plan, paid up front.
	PeriodTypePrepaid = "PREPAID"
)

// The cancellation reasons RevenueCat reports on a CANCELLATION event.
const (
	// CancelReasonUnsubscribe is the subscriber switching auto-renew off.
	CancelReasonUnsubscribe = "UNSUBSCRIBE"
	// CancelReasonBillingError is the store cancelling after it could not
	// collect.
	CancelReasonBillingError = "BILLING_ERROR"
	// CancelReasonDeveloperInitiated is the developer cancelling.
	CancelReasonDeveloperInitiated = "DEVELOPER_INITIATED"
	// CancelReasonPriceIncrease is the subscriber declining a price increase.
	CancelReasonPriceIncrease = "PRICE_INCREASE"
	// CancelReasonCustomerSupport is a support-issued refund.
	CancelReasonCustomerSupport = "CUSTOMER_SUPPORT"
	// CancelReasonUnknown is a cancellation the store gave no reason for.
	CancelReasonUnknown = "UNKNOWN"
)

// subscriptionStatuses maps every RevenueCat event type that reports a
// subscription standing onto capitalism's vocabulary.
//
// It is keyed on the event type because RevenueCat has no status field: where
// Stripe sends an object saying "past_due", RevenueCat sends an event whose
// type is the standing. That is the fold the second adapter was supposed to
// test, and the vocabulary absorbed it — eleven event types land on five of the
// eight values without a new constant.
//
// Three of the eight go unused, and that is a fact about the store rather than
// a gap here. Incomplete and IncompleteExpired describe a first payment the
// processor is still waiting on, and Apple and Google do not hand RevenueCat a
// subscription until the store has already collected; Unpaid describes a
// processor that has stopped retrying without ending the subscription, which is
// a state neither store leaves a subscription in — a failed charge becomes
// BILLING_ISSUE and then either recovers or expires.
var subscriptionStatuses = map[string]capitalism.SubscriptionStatus{
	EventTypeInitialPurchase:           capitalism.SubscriptionStatusActive,
	EventTypeRenewal:                   capitalism.SubscriptionStatusActive,
	EventTypeUncancellation:            capitalism.SubscriptionStatusActive,
	EventTypeNonRenewingPurchase:       capitalism.SubscriptionStatusActive,
	EventTypeProductChange:             capitalism.SubscriptionStatusActive,
	EventTypeSubscriptionExtended:      capitalism.SubscriptionStatusActive,
	EventTypeRefundReversed:            capitalism.SubscriptionStatusActive,
	EventTypeTemporaryEntitlementGrant: capitalism.SubscriptionStatusActive,

	// A cancellation is not the end of access. RevenueCat reports it when
	// auto-renew is switched off, and the subscriber keeps the period they paid
	// for until EXPIRATION arrives; cancellationsThatEndAccess names the
	// reasons that do end it now. Mapping the type straight onto Canceled would
	// lock out every subscriber the moment they stopped their renewal, for the
	// remainder of a period they are still entitled to.
	EventTypeCancellation: capitalism.SubscriptionStatusActive,

	// A failed charge that the store is still retrying, which is what PastDue
	// says. It becomes an EXPIRATION if the retries and any grace period run
	// out.
	EventTypeBillingIssue: capitalism.SubscriptionStatusPastDue,

	// Scheduled to pause at the end of the current period. Paused is the right
	// value even though the pause has not started, because the alternative is
	// reporting Active and never sending anything when it does begin —
	// RevenueCat has no "the pause started" event.
	EventTypeSubscriptionPaused: capitalism.SubscriptionStatusPaused,

	// Access has ended. This is the one event that means revoke.
	EventTypeExpiration: capitalism.SubscriptionStatusCanceled,
}

// eventsWithoutSubscription are the event types RevenueCat documents that
// report no subscription standing at all.
//
// They are enumerated rather than left to the table's default because the two
// silences are different facts, and capitalism.Event keeps them apart: a
// delivery listed here comes back with a nil Subscription, and an event type
// nobody has ever seen comes back with SubscriptionStatusUnknown and a log
// line. Folding the second into the first would make a status RevenueCat added
// last week look exactly like a test event.
var eventsWithoutSubscription = map[string]struct{}{
	EventTypeTest:                         {},
	EventTypeTransfer:                     {},
	EventTypeSubscriberAlias:              {},
	EventTypeInvoiceIssuance:              {},
	EventTypeVirtualCurrencyTransaction:   {},
	EventTypeExperimentEnrollment:         {},
	EventTypePurchaseRedeemed:             {},
	EventTypePriceIncreaseConsentRequired: {},
	EventTypePriceIncreaseConsentApproved: {},
}

// cancellationsThatEndAccess are the cancel_reason values on a CANCELLATION
// event that mean the subscription is over now, rather than at the end of a
// period already paid for.
//
// The set names the endings rather than the survivals, so an unrecognized or
// absent reason leaves the subscription Active. That direction is deliberate:
// RevenueCat sends EXPIRATION when access actually ends, so treating an
// unfamiliar cancellation as still-entitled is an error a later event corrects,
// while treating it as ended locks out a paid-up subscriber and nothing ever
// arrives to say otherwise. CancelReasonUnknown — the store declining to say
// why — takes that same benign path.
var cancellationsThatEndAccess = map[string]struct{}{
	CancelReasonBillingError:       {},
	CancelReasonDeveloperInitiated: {},
	CancelReasonCustomerSupport:    {},
}

// MapSubscriptionStatus translates a RevenueCat event type onto capitalism's
// vocabulary, reporting whether it recognized a subscription standing.
//
// It takes the event type where capitalism/stripe's namesake takes a status,
// because that is where RevenueCat puts the same information; the pair of
// returns means what it means there. An event type RevenueCat adds after this
// module was built maps to capitalism.SubscriptionStatusUnknown and reports
// false, rather than to whichever known value a default arm happened to name,
// so a caller can tell "RevenueCat says this subscription expired" from
// "RevenueCat says something we have never seen" — and only the first is
// grounds for cutting off an account.
//
// An event type that carries no subscription standing at all — TEST, TRANSFER,
// an experiment enrollment — likewise reports false, because the question this
// answers is what standing the event reports and the answer for those is none.
// HandleEventWebhook is what tells the two silences apart, and it does so on
// capitalism.Event.Subscription: nil for an event that is not about a
// subscription, and a state carrying the unknown status for one that is about a
// subscription this package could not place.
//
// The two refinements RevenueCat's payload carries beyond the type — a trial
// period, and a cancellation that really did end access — are applied by
// HandleEventWebhook on top of this, since neither is knowable from the type.
func MapSubscriptionStatus(eventType string) (capitalism.SubscriptionStatus, bool) {
	mapped, ok := subscriptionStatuses[eventType]
	if !ok {
		return capitalism.SubscriptionStatusUnknown, false
	}

	return mapped, true
}
