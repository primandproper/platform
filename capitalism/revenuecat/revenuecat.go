package revenuecat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/primandproper/platform-go/v14/capitalism"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/observability/metrics"
	"github.com/primandproper/platform-go/v14/webhooks/inbound"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// implementationName scopes this package's spans, logs, and metrics.
	implementationName = "revenuecat_payment_manager"

	// operationAttrKey tells one instrumented operation from another on this
	// package's counters and histogram.
	operationAttrKey = "capitalism.operation"

	// The operations, as they appear in the operation attribute.
	opHandleWebhook       = "handle_event_webhook"
	opCreateCustomer      = "create_customer"
	opCreatePaymentIntent = "create_payment_intent"
	opCreateSubscription  = "create_subscription"

	// maxWebhookBodyBytes bounds how much of a webhook request body we read.
	// RevenueCat event payloads are well under this, and it stops a hostile
	// client from forcing an unbounded allocation on this public endpoint.
	maxWebhookBodyBytes = 64 << 10 // 64 KiB
)

var (
	_ capitalism.PaymentManager = (*PaymentManager)(nil)

	// ErrNilConfig indicates a nil config was provided.
	ErrNilConfig = platformerrors.New("revenuecat config is nil")

	// ErrWebhookSecretNotConfigured indicates a manager built without a signing
	// secret. Unlike Stripe's, this one is refused at construction rather than
	// at the first delivery: there is no outbound half for a secretless manager
	// to still serve, so it could do nothing at all.
	ErrWebhookSecretNotConfigured = platformerrors.New("revenuecat webhook secret not configured; set the webhook secret to receive events")

	// ErrOutboundUnsupported indicates a PaymentManager operation RevenueCat has
	// no server-side equivalent for.
	//
	// It is a named error rather than a zero value and a nil error for the
	// reason capitalism.ErrPaymentsDisabled exists: an empty customer or
	// subscription ID is something a caller will happily persist, and the first
	// sign of trouble is a record pointing at nothing. A deployment that charges
	// on the web as well as on mobile runs capitalism/stripe alongside this, one
	// adapter per endpoint.
	ErrOutboundUnsupported = platformerrors.New("revenuecat has no server-side purchase API; store purchases are made on the device")
)

type (
	// PaymentManager is the RevenueCat capitalism.PaymentManager implementation.
	// It is exported, and returned by NewPaymentManager, so a caller who has
	// chosen RevenueCat can depend on that choice rather than on the interface
	// every payment processor shares — which here means depending on a manager
	// that is honest about being inbound-only.
	PaymentManager struct {
		o11y        observability.Observer
		instruments *metrics.OperationSet
		verifier    inbound.Verifier
	}

	// webhookEnvelope is the outer object every RevenueCat delivery arrives in.
	//
	// Event is held as raw JSON rather than decoded in the same pass so that
	// capitalism.Event.Payload can carry exactly the bytes RevenueCat sent for
	// the event — the counterpart of Stripe's data object — rather than a
	// re-encoding of a struct this package chose the fields of.
	webhookEnvelope struct {
		APIVersion string          `json:"api_version"`
		Event      json.RawMessage `json:"event"`
	}

	// webhookEvent is the part of RevenueCat's event object this adapter reads.
	//
	// It is deliberately a small subset. Everything else a consumer might want —
	// the price, the store, the entitlement IDs, whatever RevenueCat adds next —
	// is in capitalism.Event.Payload, to be decoded by whoever needs it and on
	// their own schedule. Growing this struct to mirror the provider's would
	// make every field RevenueCat adds a change here.
	webhookEvent struct {
		Type                  string `json:"type"`
		ID                    string `json:"id"`
		AppUserID             string `json:"app_user_id"`
		OriginalAppUserID     string `json:"original_app_user_id"`
		TransactionID         string `json:"transaction_id"`
		OriginalTransactionID string `json:"original_transaction_id"`
		ProductID             string `json:"product_id"`
		PeriodType            string `json:"period_type"`
		CancelReason          string `json:"cancel_reason"`
	}
)

// NewPaymentManager builds a RevenueCat-backed PaymentManager.
//
// cfg.WebhookSecret is required, and is the signing secret from the webhook
// integration in RevenueCat's dashboard rather than the Authorization header
// value configured beside it — see the package doc on why only the signed mode
// is implemented.
//
// It takes no event handler. HandleEventWebhook returns the verified delivery
// as a capitalism.Event, so acting on a webhook is something the caller does
// with a return value on the request's own goroutine, rather than something it
// registers here.
func NewPaymentManager(cfg *Config, opts ...Option) (*PaymentManager, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	if cfg.WebhookSecret == "" {
		return nil, ErrWebhookSecretNotConfigured
	}

	o := newOptions(opts)

	instruments, err := metrics.NewOperationSet(o.metricsProvider, implementationName)
	if err != nil {
		return nil, err
	}

	verifier, err := inbound.NewRevenueCatVerifier(cfg.WebhookSecret)
	if err != nil {
		return nil, platformerrors.Wrap(err, "building the revenuecat webhook verifier")
	}

	return &PaymentManager{
		o11y:        observability.NewObserver(implementationName, o.logger, o.tracerProvider),
		instruments: instruments,
		verifier:    verifier,
	}, nil
}

// operationAttrs renders the operation attribute for one instrumented call.
func operationAttrs(operation string) attribute.KeyValue {
	return attribute.String(operationAttrKey, operation)
}

// HandleEventWebhook verifies an inbound RevenueCat delivery and returns what it
// says, in this module's own vocabulary.
//
// Verification runs through webhooks/inbound's RevenueCat scheme, so this module
// has one implementation of the t=…,v1= format — the same one a service that
// receives RevenueCat webhooks without a PaymentManager gets from
// inbound.NewRevenueCatVerifier.
//
// A delivery that reports a subscription standing comes back with its
// Subscription populated, its status mapped through this package's table and
// folded for a trial period or a cancellation that really did end access. A
// delivery RevenueCat documents as carrying no standing — a test event, a
// transfer — comes back with a nil Subscription. An event type this package has
// never seen comes back with capitalism.SubscriptionStatusUnknown and a log
// line naming it, because those are three different facts and only the first is
// something to act on.
//
// This does the work inline, on the request's goroutine, and so couples
// RevenueCat's ack deadline to how long the caller takes with what it returns. A
// service whose handling does anything slow should mount an inbound.Receiver
// instead, which publishes the delivery and acks; this path exists for handling
// that is genuinely fast.
func (r *PaymentManager) HandleEventWebhook(req *http.Request) (_ *capitalism.Event, err error) {
	ctx, op := r.o11y.Begin(req.Context())
	defer op.End()

	attrs := metric.WithAttributes(operationAttrs(opHandleWebhook))
	r.instruments.Attempt(ctx, attrs)
	defer op.Time(ctx, nil, r.instruments.Latency, attrs)()

	// Cap the body of this public, unauthenticated endpoint so a hostile client
	// can't exhaust memory with an arbitrarily large payload.
	payload, err := io.ReadAll(http.MaxBytesReader(nil, req.Body, maxWebhookBodyBytes))
	if err != nil {
		r.instruments.Failed(ctx, attrs)

		return nil, op.Error(err, "reading webhook body")
	}

	if err = r.verifier.Verify(ctx, req.Header, payload); err != nil {
		r.instruments.Failed(ctx, attrs)

		return nil, op.Error(err, "verifying webhook signature")
	}

	// Decoded only after verification, so nothing here ever parses bytes
	// RevenueCat did not send.
	//
	// Through encoding/json rather than this module's encoding package, which is
	// the one place an adapter must not use it: that decoder rejects unknown
	// fields, and webhookEvent names a deliberate handful of the several dozen
	// RevenueCat sends. A strict decode here would turn every delivery into an
	// error today, and every field RevenueCat adds into an outage tomorrow. What
	// this package must not silently ignore is a field it *claims* — and there
	// are none of those, because everything beyond the handful is handed back
	// undecoded as capitalism.Event.Payload.
	var envelope webhookEnvelope
	if err = json.Unmarshal(payload, &envelope); err != nil {
		r.instruments.Failed(ctx, attrs)

		return nil, op.Error(err, "decoding webhook envelope")
	}

	var event webhookEvent
	if len(envelope.Event) > 0 {
		// A delivery signed under the right secret can still be shaped however its
		// sender liked — this is a public endpoint — so an absent event object is
		// left as the zero value rather than fed to a decoder as `null`.
		if err = json.Unmarshal(envelope.Event, &event); err != nil {
			r.instruments.Failed(ctx, attrs)

			return nil, op.Error(err, "decoding webhook event")
		}
	}

	op.Set("revenuecat.api_version", envelope.APIVersion).
		Set("revenuecat.event_id", event.ID).
		Set("revenuecat.event_type", event.Type)

	out := &capitalism.Event{ID: event.ID, Type: event.Type, Payload: envelope.Event}

	if _, absent := eventsWithoutSubscription[event.Type]; absent {
		// Documented as carrying no subscription standing. Nil rather than the
		// unknown status: "this event is not about a subscription" is a fact this
		// package knows, not one it failed to establish.
		return out, nil
	}

	status, known := subscriptionStatus(&event)

	out.Subscription = &capitalism.SubscriptionState{
		ID:             subscriptionID(&event),
		CustomerID:     customerID(&event),
		Status:         status,
		ProviderStatus: event.Type,
	}

	op.Set("revenuecat.subscription_id", out.Subscription.ID).
		Set("revenuecat.app_user_id", out.Subscription.CustomerID).
		Set("revenuecat.product_id", event.ProductID).
		Set("revenuecat.period_type", event.PeriodType).
		Set("capitalism.subscription_status", out.Subscription.Status.String())

	if !known {
		// Logged rather than errored: the delivery is genuine and the caller is
		// handed the raw event type, so refusing it would drop an event RevenueCat
		// will eventually stop retrying. What it needs is an entry in the mapping
		// table, and this is the line that says which one.
		op.Logger().WithValue("revenuecat.event_type", event.Type).
			Info("unrecognized subscription event type")
	}

	return out, nil
}

// subscriptionStatus folds what the event carries beyond its type into the
// status the type maps to.
//
// The two folds are the ones RevenueCat's shape makes necessary and the event
// type cannot express: a purchase or renewal inside a free trial is Trialing
// rather than Active, and a cancellation for one of the reasons that really did
// end access is Canceled rather than the still-entitled Active the type maps to.
// Both are documented at their tables.
func subscriptionStatus(event *webhookEvent) (capitalism.SubscriptionStatus, bool) {
	status, known := MapSubscriptionStatus(event.Type)
	if !known {
		return status, false
	}

	if event.Type == EventTypeCancellation {
		if _, ended := cancellationsThatEndAccess[event.CancelReason]; ended {
			return capitalism.SubscriptionStatusCanceled, true
		}
	}

	if status == capitalism.SubscriptionStatusActive && event.PeriodType == PeriodTypeTrial {
		return capitalism.SubscriptionStatusTrialing, true
	}

	return status, true
}

// subscriptionID is the identifier the subscription keeps across renewals.
//
// RevenueCat mints a fresh transaction_id for every renewal and holds
// original_transaction_id fixed, so the original is the one an application can
// store against an account and still recognize a year later. The current
// transaction is the fallback for the purchases that have no original — a
// non-renewing purchase is its own first transaction.
func subscriptionID(event *webhookEvent) string {
	if event.OriginalTransactionID != "" {
		return event.OriginalTransactionID
	}

	return event.TransactionID
}

// customerID is the RevenueCat-side customer the subscription bills.
//
// It is app_user_id, which is the value the application itself set through the
// SDK and therefore the handle it already stores. original_app_user_id is the
// fallback rather than the primary: it is what the customer was called before an
// alias or a transfer, which is the right answer only when there is no current
// one to have.
func customerID(event *webhookEvent) string {
	if event.AppUserID != "" {
		return event.AppUserID
	}

	return event.OriginalAppUserID
}

// CreateCustomer reports ErrOutboundUnsupported.
//
// RevenueCat's customers come into existence when its SDK first identifies one
// on the device, under an app user ID the application chose. There is nothing
// for a server to create and no provider-assigned ID to return.
func (r *PaymentManager) CreateCustomer(ctx context.Context, _ *capitalism.CustomerCreationInput) (string, error) {
	return "", r.unsupported(ctx, opCreateCustomer, "creating customer")
}

// CreatePaymentIntent reports ErrOutboundUnsupported.
//
// A payment intent is a charge this side initiates, and RevenueCat initiates
// none: the money moves through Apple's and Google's purchase flows, which run
// in the store's own UI on the device.
func (r *PaymentManager) CreatePaymentIntent(ctx context.Context, _ *capitalism.PaymentIntentCreationInput) (*capitalism.PaymentIntent, error) {
	return nil, r.unsupported(ctx, opCreatePaymentIntent, "creating payment intent")
}

// CreateSubscription reports ErrOutboundUnsupported.
//
// A mobile subscription is created by a store purchase the subscriber completes
// on the device. RevenueCat learns about it and tells this side, which is the
// direction HandleEventWebhook serves.
func (r *PaymentManager) CreateSubscription(ctx context.Context, _ *capitalism.SubscriptionCreationInput) (string, error) {
	return "", r.unsupported(ctx, opCreateSubscription, "creating subscription")
}

// unsupported records one refused outbound call and returns the error it is
// refused with.
//
// The call is still counted, and counted as a failure, because a deployment
// calling an operation this provider does not have is a wiring mistake somebody
// should be able to see on a graph rather than only in a log line.
func (r *PaymentManager) unsupported(ctx context.Context, operation, description string) error {
	ctx, op := r.o11y.Begin(ctx)
	defer op.End()

	attrs := metric.WithAttributes(operationAttrs(operation))
	r.instruments.Attempt(ctx, attrs)
	r.instruments.Failed(ctx, attrs)

	return op.Error(ErrOutboundUnsupported, "%s", description)
}
