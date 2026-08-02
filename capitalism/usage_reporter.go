package capitalism

import (
	"context"
	"time"
)

type (
	// UsageReporter posts metered usage to a billing provider, so that what a
	// customer consumed becomes what a customer is invoiced.
	//
	// It is a separate interface from PaymentManager rather than four more
	// methods on it, because the two are wanted by different processes. Charging
	// happens on a request path in an API server; usage reporting happens on a
	// cron tick in a worker, and a deployment that does one usually has no
	// business holding the credentials for the other.
	//
	// The seam exists so that metering can reach a provider's usage API without
	// importing the provider's SDK. That direction is load-bearing: usage
	// reporting is where a retry turns into a duplicate charge, and the code
	// that has to get that right should be testable without a Stripe key.
	UsageReporter interface {
		// ReportUsage posts one usage record. Implementations must pass
		// input.IdempotencyKey to the provider, so that a retried report is a
		// no-op at the provider rather than a second charge.
		ReportUsage(ctx context.Context, input *UsageReportInput) error
	}

	// UsageReportInput describes one usage record to post.
	UsageReportInput struct {
		// OccurredAt is when the usage happened. Providers bound how far back a
		// report may be dated and refuse a timestamp meaningfully in the future;
		// a zero value means "now", which is what a flush of freshly-accumulated
		// usage means anyway.
		OccurredAt time.Time

		// Metadata is provider-side annotation. It is not used for pricing, and
		// not every provider accepts it. Treat it as best-effort context for the
		// providers and custom reporters that can store it, never as somewhere to
		// put a fact only this field would record.
		//
		// A provider whose usage payload is a flat key/value map — Stripe's is —
		// reserves some of that namespace for the fields that decide who is
		// billed and how much. Adapters refuse a metadata key that collides with
		// one of those rather than letting annotation rewrite the charge.
		Metadata map[string]string

		// CustomerID is the provider-side customer the usage is billed to — for
		// Stripe, the `cus_…` the meter's customer mapping resolves against.
		//
		// It is a provider handle rather than an application's own subject ID
		// because only the application knows the mapping, and a library that
		// guessed it would post one customer's usage onto another's invoice.
		CustomerID string

		// MeterName is the provider-side meter the usage counts against — for
		// Stripe, the `event_name` of the billing meter.
		//
		// It is deliberately not assumed to equal the application's own name for
		// the same meter. The provider's meters are configured in the provider's
		// dashboard by whoever owns pricing, and the two names drift apart the
		// first time a plan is renamed; the application supplies the pair
		// together so the mapping stays in one place.
		MeterName string

		// IdempotencyKey makes the post safely retryable. It is required rather
		// than optional, unlike the create inputs in this package: those are
		// driven by a user action that a person can see the result of, and this
		// one is driven by a retry loop that nobody watches.
		IdempotencyKey string

		// Quantity is how much usage to add. It is an increment, not a running
		// total: providers aggregate the records within a billing period, so
		// posting a cumulative total would bill the sum of every partial total
		// ever posted.
		Quantity int64
	}
)
