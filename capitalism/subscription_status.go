package capitalism

// SubscriptionStatus is where a subscription stands with the payment
// processor, in this module's own vocabulary.
//
// It exists because the inbound half of this seam previously had none. A
// webhook arrived as an event type and a blob of provider JSON, so every
// consumer that wanted to know what had changed about a customer's standing
// decoded that blob with the provider's SDK and wrote its own switch over the
// provider's words. That switch is vendor knowledge, it is written once per
// consumer, and it is the kind of duplicate that can be wrong: deciding which
// of Stripe's eight statuses means "still entitled" is a judgement, and getting
// it wrong leaves an account entitled after it lapsed, or locked out while the
// processor considers it current, until somebody notices in support.
//
// The values read like Stripe's because Stripe's are the set the rest of the
// industry copied, not because this type is Stripe's. Each adapter maps its
// provider's vocabulary onto these explicitly — capitalism/stripe is the only
// thing in this module that knows what "past_due" means — so a provider that
// says "deleted" arrives here as SubscriptionStatusCanceled rather than as a
// ninth constant.
//
// This is not identity.BillingStatus and does not replace it. This type is what
// the processor reports; identity.BillingStatus is the coarse standing an
// application gates on, and includes BillingSuspended, which is an operator
// action no processor reports. Which of these statuses an application treats as
// paid is policy — whether a trial may still write is the application's rule,
// which is why identity stores an answer rather than deriving one. The mapping
// between the two vocabularies belongs to the application, and this type is
// what makes it a switch over a closed, documented set instead of over strings
// from an SDK.
type SubscriptionStatus string

const (
	// SubscriptionStatusUnknown is a status no adapter recognized, and the zero
	// value.
	//
	// It is a named value rather than a fallback onto one of the real ones
	// because "the provider said something we have not seen" and "the provider
	// said the subscription is canceled" are different facts, and quietly
	// rendering the first as the second is how a processor's new status becomes
	// a wrongly-locked-out account. A consumer that sees it should read
	// SubscriptionState.ProviderStatus, which carries what actually arrived.
	SubscriptionStatusUnknown SubscriptionStatus = ""

	// SubscriptionStatusIncomplete is a subscription whose first payment has not
	// yet succeeded, and which the processor is still waiting on.
	SubscriptionStatusIncomplete SubscriptionStatus = "incomplete"

	// SubscriptionStatusIncompleteExpired is a subscription whose first payment
	// never succeeded within the processor's window. It is terminal: nothing was
	// ever collected and nothing will be.
	SubscriptionStatusIncompleteExpired SubscriptionStatus = "incomplete_expired"

	// SubscriptionStatusTrialing is a subscription inside its trial window,
	// which the processor considers current and has charged nothing for.
	SubscriptionStatusTrialing SubscriptionStatus = "trialing"

	// SubscriptionStatusActive is a subscription in good standing and paid up.
	SubscriptionStatusActive SubscriptionStatus = "active"

	// SubscriptionStatusPastDue is a subscription whose latest invoice went
	// unpaid past its due date and which the processor is still retrying.
	SubscriptionStatusPastDue SubscriptionStatus = "past_due"

	// SubscriptionStatusCanceled is a subscription that has ended, whether the
	// customer cancelled it or the processor gave up collecting.
	SubscriptionStatusCanceled SubscriptionStatus = "canceled"

	// SubscriptionStatusUnpaid is a subscription the processor has stopped
	// retrying but has not ended, leaving invoices outstanding.
	SubscriptionStatusUnpaid SubscriptionStatus = "unpaid"

	// SubscriptionStatusPaused is a subscription deliberately suspended at the
	// processor, collecting nothing and expected to resume.
	SubscriptionStatusPaused SubscriptionStatus = "paused"
)

// Known reports whether s is a status this module recognizes, which is the
// check that separates a mapped status from one an adapter could not place.
//
// SubscriptionStatusUnknown is deliberately not known. A caller switching on a
// status should branch on this first, so that a status the provider added after
// this module was built takes the "we do not know" path rather than the
// default arm of a switch written when there were eight.
func (s SubscriptionStatus) Known() bool {
	switch s {
	case SubscriptionStatusIncomplete,
		SubscriptionStatusIncompleteExpired,
		SubscriptionStatusTrialing,
		SubscriptionStatusActive,
		SubscriptionStatusPastDue,
		SubscriptionStatusCanceled,
		SubscriptionStatusUnpaid,
		SubscriptionStatusPaused:
		return true
	default:
		return false
	}
}

// String renders the status. An unknown status renders as "unknown" rather than
// as the empty string it is stored as, so a log line or span attribute says
// which case it was instead of going blank.
func (s SubscriptionStatus) String() string {
	if s == SubscriptionStatusUnknown {
		return "unknown"
	}

	return string(s)
}
