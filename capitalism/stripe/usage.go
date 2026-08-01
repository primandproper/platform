package stripe

import (
	"context"

	"github.com/primandproper/platform-go/v9/capitalism"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"

	"github.com/stripe/stripe-go/v75"
	"github.com/stripe/stripe-go/v75/client"
)

const usageImplementationName = "stripe_usage_reporter"

var (
	_ capitalism.UsageReporter = (*stripeUsageReporter)(nil)

	// ErrEmptySubscriptionItem indicates a report with no provider handle to post
	// against. Stripe puts the subscription item in the request path, so an empty
	// one would be a request to a different endpoint entirely rather than a
	// validation error at the API.
	ErrEmptySubscriptionItem = platformerrors.New("empty stripe subscription item ID")

	// ErrEmptyUsageIdempotencyKey indicates a report with no idempotency key.
	//
	// It is refused rather than defaulted. A usage post with no key is a post
	// that double-bills on retry, and the retry is not optional — it is what a
	// flush loop does when the network blinks. Generating a key here would make
	// every attempt distinct, which is precisely the wrong answer.
	ErrEmptyUsageIdempotencyKey = platformerrors.New("empty stripe usage idempotency key")
)

// stripeUsageReporter posts usage records to Stripe's metered billing API.
type stripeUsageReporter struct {
	o11y   observability.Observer
	client *client.API
}

// NewStripeUsageReporter builds a Stripe-backed UsageReporter.
//
// Unlike NewStripePaymentManager, the API key is required: there is no inbound
// path here, so a reporter without one could do nothing at all and would fail on
// its first flush rather than at startup.
func NewStripeUsageReporter(cfg *Config, opts ...Option) (capitalism.UsageReporter, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	if cfg.APIKey == "" {
		return nil, ErrAPIKeyNotConfigured
	}

	o := newOptions(opts)

	sc := &client.API{}
	sc.Init(cfg.APIKey, nil)

	return &stripeUsageReporter{
		client: sc,
		o11y:   observability.NewObserver(usageImplementationName, o.logger, o.tracerProvider),
	}, nil
}

// ReportUsage posts one usage record, incrementing the subscription item's usage
// for the current billing period.
//
// The action is always increment. Stripe's other action, set, overwrites the
// usage at a timestamp — which sounds appealing for a flush that knows the
// running total, and is a trap: two flushes for the same period with different
// timestamps would leave the smaller one standing beside the larger rather than
// replacing it. An increment carrying the delta since the last flush, under a
// key derived from the flush's sequence number, is the combination that survives
// a retry.
func (s *stripeUsageReporter) ReportUsage(ctx context.Context, input *capitalism.UsageReportInput) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if input == nil {
		return op.Error(platformerrors.ErrNilInputParameter, "reporting usage")
	}

	if input.SubscriptionItemID == "" {
		return op.Error(ErrEmptySubscriptionItem, "reporting usage")
	}

	if input.IdempotencyKey == "" {
		return op.Error(ErrEmptyUsageIdempotencyKey, "reporting usage")
	}

	op.Set("stripe.subscription_item_id", input.SubscriptionItemID).
		Set("stripe.usage_quantity", input.Quantity)

	action := stripe.UsageRecordActionIncrement
	params := &stripe.UsageRecordParams{
		SubscriptionItem: &input.SubscriptionItemID,
		Action:           &action,
		Quantity:         &input.Quantity,
	}

	if input.OccurredAt.IsZero() {
		// "now" rather than a locally computed timestamp: Stripe rejects a usage
		// record dated in the future, and a worker whose clock runs a few seconds
		// fast would otherwise have every flush refused.
		params.TimestampNow = new(true)
	} else {
		params.Timestamp = new(input.OccurredAt.Unix())
	}

	// input.Metadata is deliberately not forwarded. Stripe's usage record object
	// carries none — the create endpoint accepts only the item, action, quantity,
	// and timestamp — and the SDK refuses the request outright rather than
	// ignoring the field. What a usage record is about is recoverable from the
	// subscription item it was posted against, so nothing is lost that a
	// reconciliation would need.
	applyRequestParams(&params.Params, ctx, input.IdempotencyKey)

	record, err := s.client.UsageRecords.New(params)
	if err != nil {
		return op.Error(err, "reporting usage")
	}

	op.Set("stripe.usage_record_id", record.ID)

	return nil
}
