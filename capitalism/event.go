package capitalism

type (
	// Event is a verified inbound webhook delivery, in this module's own
	// vocabulary.
	//
	// It lives here rather than in an adapter package because a consumer wiring
	// a webhook endpoint should not have to import capitalism/stripe to name the
	// thing its handler receives — that is the same binding UsageReporter was
	// shaped to avoid on the way out. The adapters translate; nothing about this
	// type is provider-specific.
	//
	// Payload stays raw for the same reason it always was: typing it on a
	// provider SDK would pin every consumer of this module to that SDK's exact
	// major, and turn each of its major bumps into a breaking change for callers
	// who never mention the provider. Subscription covers the one thing a
	// consumer should not have to decode provider JSON to learn; anything richer
	// is decoded from Payload, with whatever SDK version the consumer chooses,
	// on its own schedule.
	Event struct {
		// Subscription is the subscription standing this delivery reports, or
		// nil when the event carries none.
		//
		// It is a pointer, and nil is a third case rather than a stand-in for
		// SubscriptionStatusUnknown: "this event is not about a subscription"
		// (a succeeded payment intent, an updated account) and "this event is
		// about a subscription whose status we could not place" are different
		// facts, and a consumer that reconciles billing on the second must not
		// be handed the first.
		Subscription *SubscriptionState

		// ID is the provider's event identifier, stable for deduplication.
		ID string

		// Type is the provider's own event type, e.g. "payment_intent.succeeded".
		// It is deliberately not mapped onto a vocabulary of this module's own:
		// unlike a subscription status, the event type space is open-ended, and
		// a consumer switching on it has already accepted that it is reading the
		// provider's words.
		Type string

		// Payload is the raw JSON of the event's data object, exactly as it
		// arrived.
		Payload []byte
	}

	// SubscriptionState is what a delivery says about one subscription.
	//
	// It carries the identifiers alongside the status because a status with
	// nothing attached is not actionable: an application updating an account's
	// billing needs to know whose standing moved, and the alternative is
	// re-decoding the payload with a provider SDK to find out — which is the
	// work this type exists to remove.
	SubscriptionState struct {
		// ID is the provider's subscription identifier.
		ID string

		// CustomerID is the provider-side customer the subscription bills, which
		// is the handle an application stores against its own account. It is
		// empty when the delivery did not name one.
		CustomerID string

		// Status is the provider's reported status, mapped onto this module's
		// vocabulary. It is SubscriptionStatusUnknown when the adapter did not
		// recognize what the provider reported.
		Status SubscriptionStatus

		// ProviderStatus is the status exactly as the provider spelled it.
		//
		// It is kept even when Status is known, because it is what makes an
		// unrecognized status diagnosable: a consumer that logs an unknown
		// status can say which word it was, and the fix is one entry in the
		// adapter's mapping table rather than a bisect through provider JSON.
		ProviderStatus string
	}
)
