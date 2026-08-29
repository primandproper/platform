/*
Package revenuecat translates RevenueCat's mobile subscription webhooks into
capitalism's vocabulary.

It is the second adapter behind capitalism.PaymentManager, and it is a sibling
of capitalism/stripe rather than a variation on it: the two are near-identical
in shape because each is a translation between one vendor's words and this
module's, and the lines that differ are the ones a reader opened the file for.
See llm/doc.go for the long form of why that duplication is kept.

# Inbound only

RevenueCat has no server-side purchase API to call. A mobile subscription is
created by a purchase the store completes on the device, and RevenueCat's
customers come into existence when its SDK first identifies one; there is no
payment intent at all, because the money moves through Apple and Google. The
three outbound PaymentManager methods therefore report ErrOutboundUnsupported
rather than a zero value and a nil error, for the same reason the noop manager
reports capitalism.ErrPaymentsDisabled: an empty customer or subscription ID is
something a caller will happily persist, and the first sign of trouble is a
record pointing at nothing.

A deployment that charges on the web and on mobile runs both adapters, one per
endpoint, and that is the shape to reach for — not a merged manager, which
would have to pretend one provider could answer for the other.

There is likewise no UsageReporter here, and the absence is named rather than
merely true. RevenueCat prices whole subscriptions, not metered consumption, so
there is no meter to post to; usage.go carries that reason and
ErrUsageReportingUnsupported, which capitalismcfg's usage reporter constructor
refuses with when RevenueCat is the selected provider. It is a second sentinel
because it is a second reason: the outbound methods above refuse because the
store owns the purchase, and a usage report is refused because there is nothing
to meter.

# What the events say, and what they do not

RevenueCat has no subscription status field. Where Stripe sends an object
carrying "past_due", RevenueCat sends an event whose *type* is the standing —
BILLING_ISSUE, EXPIRATION, SUBSCRIPTION_PAUSED — so the mapping table here is
keyed on the event type, and SubscriptionState.ProviderStatus carries that type
rather than a status the payload does not contain.

Two of RevenueCat's folds do not fall out of the type alone, and both are
applied on top of the table:

  - A purchase or renewal inside a free trial is period_type TRIAL, and lands
    on capitalism.SubscriptionStatusTrialing rather than Active. The other
    period types — INTRO, PROMOTIONAL, PREPAID, NORMAL — are all a paid-up
    subscription as far as this vocabulary is concerned.

  - CANCELLATION does not mean access ended. RevenueCat reports it when
    auto-renew is switched off, and the subscriber keeps what they paid for
    until EXPIRATION arrives; the exceptions are the cancel_reason values that
    do end it now — a refund, a developer cancellation, a store cancellation
    after a billing failure. See cancellationsThatEndAccess.

An event type this package has never seen is reported as
capitalism.SubscriptionStatusUnknown with the type in ProviderStatus, and
logged. It is not guessed onto a neighboring value and it is not dropped:
RevenueCat adds event types, and the fix for a new one is an entry in the table
rather than a bisect through provider JSON.

# Verification

Deliveries are verified through webhooks/inbound's RevenueCat scheme, so this
module has one implementation of the t=…,v1= format that Stripe published and
RevenueCat adopted. RevenueCat also offers a dashboard-configured Authorization
header, which proves the sender knew a secret and says nothing about the body
it arrived with; only the signed mode is implemented, and turning it on is a
toggle on the same dashboard page.
*/
package revenuecat
