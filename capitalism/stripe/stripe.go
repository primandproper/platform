package stripe

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v13/capitalism"
	"github.com/primandproper/platform-go/v13/encoding"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/webhooks/inbound"

	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/client"
)

const (
	implementationName = "stripe_payment_manager"
	// subscriptionEventPrefix is the prefix every Stripe event whose data object is a
	// Subscription shares — created, updated, deleted, paused, resumed, trial_will_end and
	// the pending-update pair. Matching the prefix rather than listing the eight is what
	// keeps a subscription event Stripe adds later from arriving with its status undecoded.
	subscriptionEventPrefix = "customer.subscription."
	// maxWebhookBodyBytes bounds how much of a webhook request body we read; Stripe
	// event payloads are well under this, and it stops a hostile client from forcing
	// an unbounded allocation on this public endpoint.
	maxWebhookBodyBytes = 64 << 10 // 64 KiB
)

var (
	_ capitalism.PaymentManager = (*PaymentManager)(nil)

	// ErrNilConfig indicates a nil config was provided.
	ErrNilConfig = platformerrors.New("stripe config is nil")
	// ErrAPIKeyNotConfigured indicates an outbound operation was attempted without a Stripe API
	// key. The webhook path needs only WebhookSecret, so the key is optional at construction;
	// outbound operations require it.
	ErrAPIKeyNotConfigured = platformerrors.New("stripe API key not configured; set the API key to use outbound operations")

	// ErrWebhookSecretNotConfigured indicates an inbound webhook arrived at a manager built
	// without one. The outbound operations need only APIKey, so the secret is optional at
	// construction; the webhook path requires it.
	//
	// It is its own error because the alternative — verifying under an empty secret — rejects
	// every delivery with a signature error, which reads as Stripe's fault rather than as a
	// missing environment variable.
	ErrWebhookSecretNotConfigured = platformerrors.New("stripe webhook secret not configured; set the webhook secret to receive events")
)

type (
	// PaymentManager is the Stripe capitalism.PaymentManager implementation. It is
	// exported, and returned by NewPaymentManager, so a caller who has chosen
	// Stripe can depend on that choice rather than on the interface every payment
	// processor shares.
	PaymentManager struct {
		o11y           observability.Observer
		encoderDecoder encoding.ServerEncoderDecoder
		client         *client.API
		instruments    *instruments
		verifier       inbound.Verifier
	}
)

// NewPaymentManager builds a Stripe-backed PaymentManager. When cfg.APIKey is set, an API
// client is initialized for outbound operations; when cfg.WebhookSecret is set, a verifier is
// built for the inbound webhook path. Either half works without the other.
//
// It takes no event handler. HandleEventWebhook returns the verified delivery as a
// capitalism.Event, so acting on a webhook is something the caller does with a return value
// on the request's own goroutine, rather than something it registers here — which is what
// forced a consumer that never otherwise names Stripe to import this package for a callback
// signature.
func NewPaymentManager(cfg *Config, opts ...Option) (*PaymentManager, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	o := newOptions(opts)

	instruments, err := newInstruments(o.metricsProvider)
	if err != nil {
		return nil, err
	}

	m := &PaymentManager{
		encoderDecoder: encoding.NewServerEncoderDecoder(encoding.ContentTypeJSON, encoding.WithLogger(o.logger), encoding.WithTracerProvider(o.tracerProvider)),
		o11y:           observability.NewObserver(implementationName, o.logger, o.tracerProvider),
		instruments:    instruments,
	}

	if cfg.APIKey != "" {
		sc := &client.API{}
		sc.Init(cfg.APIKey, nil)
		m.client = sc
	}

	if cfg.WebhookSecret != "" {
		verifier, verifierErr := inbound.NewStripeVerifier(cfg.WebhookSecret)
		if verifierErr != nil {
			return nil, platformerrors.Wrap(verifierErr, "building the stripe webhook verifier")
		}

		m.verifier = verifier
	}

	return m, nil
}

// HandleEventWebhook verifies an inbound Stripe delivery and returns what it says, in this
// module's own vocabulary.
//
// Verification runs through webhooks/inbound's Stripe scheme rather than through stripe-go's,
// so this module has one implementation of the t=…,v1=… format — the same one a service that
// receives Stripe webhooks without a PaymentManager gets from inbound.NewStripeVerifier.
//
// A delivery about a subscription comes back with its Subscription populated, its status
// mapped through this package's table, so the caller reconciling an account's standing does
// not decode Stripe JSON to find out what changed. Everything else comes back with the raw
// payload and nothing decoded, which is all this package can honestly say about it.
//
// This does the work inline, on the request's goroutine, and so couples Stripe's ack deadline
// to how long the caller takes with what it returns. A service whose handling does anything
// slow should mount an inbound.Receiver instead, which publishes the delivery and acks; this
// path exists for handling that is genuinely fast.
func (s *PaymentManager) HandleEventWebhook(req *http.Request) (_ *capitalism.Event, err error) {
	ctx, op := s.o11y.Begin(req.Context())
	defer op.End()

	startedAt := time.Now()
	defer func() { s.instruments.record(ctx, opHandleWebhook, startedAt, err) }()

	if s.verifier == nil {
		return nil, op.Error(ErrWebhookSecretNotConfigured, "verifying webhook signature")
	}

	// Cap the body of this public, unauthenticated endpoint so a hostile client
	// can't exhaust memory with an arbitrarily large payload.
	payload, err := io.ReadAll(http.MaxBytesReader(nil, req.Body, maxWebhookBodyBytes))
	if err != nil {
		return nil, op.Error(err, "reading webhook body")
	}

	if err = s.verifier.Verify(ctx, req.Header, payload); err != nil {
		return nil, op.Error(err, "verifying webhook signature")
	}

	// Decoded only after verification, so nothing here ever parses bytes Stripe did not send.
	var event stripe.Event
	if err = s.encoderDecoder.DecodeBytes(ctx, payload, &event); err != nil {
		return nil, op.Error(err, "decoding webhook event")
	}

	op.Set("stripe.event_id", event.ID).Set("stripe.event_type", event.Type)

	// Every Stripe event carries a data object, but the field is a pointer and this is a
	// public endpoint: a delivery signed under the right secret can still be shaped however
	// its sender liked, and dereferencing on trust would turn that into a panic.
	var raw []byte
	if event.Data != nil {
		raw = event.Data.Raw
	}

	out := &capitalism.Event{ID: event.ID, Type: string(event.Type), Payload: raw}

	switch {
	case event.Type == stripe.EventTypePaymentIntentSucceeded:
		var paymentIntent stripe.PaymentIntent
		if err = s.encoderDecoder.DecodeBytes(ctx, raw, &paymentIntent); err != nil {
			return nil, op.Error(err, "decoding payment intent")
		}

		op.Set("stripe.payment_intent_id", paymentIntent.ID).
			Set("stripe.amount", paymentIntent.Amount).
			Set("stripe.currency", paymentIntent.Currency)
	case strings.HasPrefix(string(event.Type), subscriptionEventPrefix):
		// Every customer.subscription.* event's data object is a Subscription, including
		// the deleted one — Stripe reports a cancellation as a final state on the object
		// rather than as an absence.
		var subscription stripe.Subscription
		if err = s.encoderDecoder.DecodeBytes(ctx, raw, &subscription); err != nil {
			return nil, op.Error(err, "decoding subscription")
		}

		out.Subscription = subscriptionState(&subscription)

		op.Set("stripe.subscription_id", out.Subscription.ID).
			Set("stripe.customer_id", out.Subscription.CustomerID).
			Set("stripe.subscription_status", out.Subscription.ProviderStatus).
			Set("capitalism.subscription_status", out.Subscription.Status.String())

		if !out.Subscription.Status.Known() {
			// Logged rather than errored: the delivery is genuine and the caller is
			// handed the raw status, so refusing it would drop an event Stripe will
			// not send again. What it needs is an entry in the mapping table, and this
			// is the line that says which one.
			op.Logger().WithValue("stripe.subscription_status", out.Subscription.ProviderStatus).
				Info("unrecognized subscription status")
		}
	default:
		op.Set("event_type", event.Type)
		op.Logger().WithRequest(req).Info("Unhandled event type")
	}

	return out, nil
}

// subscriptionState projects a decoded Stripe subscription onto this module's vocabulary.
//
// Customer is read through its pointer because stripe-go leaves it nil for a payload that
// named no customer, and because the field arrives as either an ID string or an expanded
// object depending on how the endpoint is configured — stripe-go's own unmarshaller absorbs
// that difference, which is the reason this decodes through the SDK's type rather than
// through a hand-rolled struct.
func subscriptionState(subscription *stripe.Subscription) *capitalism.SubscriptionState {
	state := &capitalism.SubscriptionState{
		ID:             subscription.ID,
		ProviderStatus: string(subscription.Status),
	}

	state.Status, _ = MapSubscriptionStatus(string(subscription.Status))

	if subscription.Customer != nil {
		state.CustomerID = subscription.Customer.ID
	}

	return state
}

// CreateCustomer creates a Stripe customer.
func (s *PaymentManager) CreateCustomer(ctx context.Context, input *capitalism.CustomerCreationInput) (_ string, err error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	startedAt := time.Now()
	defer func() { s.instruments.record(ctx, opCreateCustomer, startedAt, err) }()

	if input == nil {
		return "", op.Error(platformerrors.ErrNilInputParameter, "creating customer")
	}
	if s.client == nil {
		return "", op.Error(ErrAPIKeyNotConfigured, "creating customer")
	}

	params := &stripe.CustomerParams{Metadata: input.Metadata}
	if input.Email != "" {
		params.Email = new(input.Email)
	}
	if input.Name != "" {
		params.Name = new(input.Name)
	}
	applyRequestParams(&params.Params, ctx, input.IdempotencyKey)

	customer, err := s.client.Customers.New(params)
	if err != nil {
		return "", op.Error(err, "creating customer")
	}

	op.Set("stripe.customer_id", customer.ID)

	return customer.ID, nil
}

// CreatePaymentIntent creates a Stripe payment intent.
func (s *PaymentManager) CreatePaymentIntent(ctx context.Context, input *capitalism.PaymentIntentCreationInput) (_ *capitalism.PaymentIntent, err error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	startedAt := time.Now()
	defer func() { s.instruments.record(ctx, opCreatePaymentIntent, startedAt, err) }()

	if input == nil {
		return nil, op.Error(platformerrors.ErrNilInputParameter, "creating payment intent")
	}
	if s.client == nil {
		return nil, op.Error(ErrAPIKeyNotConfigured, "creating payment intent")
	}

	params := &stripe.PaymentIntentParams{
		Amount:   new(input.Amount),
		Currency: new(input.Currency),
		Metadata: input.Metadata,
	}
	if input.CustomerID != "" {
		params.Customer = new(input.CustomerID)
	}
	if input.Description != "" {
		params.Description = new(input.Description)
	}
	applyRequestParams(&params.Params, ctx, input.IdempotencyKey)

	intent, err := s.client.PaymentIntents.New(params)
	if err != nil {
		return nil, op.Error(err, "creating payment intent")
	}

	op.Set("stripe.payment_intent_id", intent.ID)

	return &capitalism.PaymentIntent{ID: intent.ID, ClientSecret: intent.ClientSecret}, nil
}

// CreateSubscription creates a Stripe subscription for a customer on a single price.
func (s *PaymentManager) CreateSubscription(ctx context.Context, input *capitalism.SubscriptionCreationInput) (_ string, err error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	startedAt := time.Now()
	defer func() { s.instruments.record(ctx, opCreateSubscription, startedAt, err) }()

	if input == nil {
		return "", op.Error(platformerrors.ErrNilInputParameter, "creating subscription")
	}
	if s.client == nil {
		return "", op.Error(ErrAPIKeyNotConfigured, "creating subscription")
	}

	params := &stripe.SubscriptionParams{
		Customer: new(input.CustomerID),
		Items: []*stripe.SubscriptionItemsParams{
			{Price: new(input.PriceID)},
		},
		Metadata: input.Metadata,
	}
	applyRequestParams(&params.Params, ctx, input.IdempotencyKey)

	subscription, err := s.client.Subscriptions.New(params)
	if err != nil {
		return "", op.Error(err, "creating subscription")
	}

	op.Set("stripe.subscription_id", subscription.ID)

	return subscription.ID, nil
}

// applyRequestParams attaches the context and, when provided, an idempotency key to a Stripe
// request so a create is safely retryable.
func applyRequestParams(p *stripe.Params, ctx context.Context, idempotencyKey string) {
	p.Context = ctx
	if idempotencyKey != "" {
		p.SetIdempotencyKey(idempotencyKey)
	}
}
