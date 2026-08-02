package posthog

import (
	"context"
	"fmt"

	"github.com/primandproper/platform-go/v9/analytics"
	"github.com/primandproper/platform-go/v9/circuitbreaking"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"

	"github.com/posthog/posthog-go"
)

const (
	name = "posthog_event_reporter"
)

var (
	// ErrEmptyAPIToken indicates an empty API token was provided.
	ErrEmptyAPIToken = platformerrors.New("empty Posthog API token")
)

type (
	// EventReporter is a PostHog-backed EventReporter.
	EventReporter struct {
		o11y           observability.Observer
		client         posthog.Client
		eventCounter   metrics.Int64Counter
		errorCounter   metrics.Int64Counter
		circuitBreaker circuitbreaking.CircuitBreaker
	}
)

// NewPostHogEventReporter returns a new PostHog-backed EventReporter.
func NewPostHogEventReporter(apiKey string, circuitBreaker circuitbreaking.CircuitBreaker, opts ...Option) (analytics.EventReporter, error) {
	if apiKey == "" {
		return nil, ErrEmptyAPIToken
	}

	o := newOptions(opts)

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	eventCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_events", name))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating event counter")
	}

	errorCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_errors", name))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating error counter")
	}

	logger := logging.EnsureLogger(o.logger)

	phc := posthog.Config{Endpoint: "https://app.posthog.com"}
	for _, f := range o.configModifiers {
		f(&phc)
	}
	// Drive the breaker from delivery outcomes (Enqueue only buffers), unless a
	// config modifier already installed its own callback.
	if phc.Callback == nil {
		phc.Callback = &breakerCallback{
			circuitBreaker: circuitBreaker,
			errorCounter:   errorCounter,
			logger:         logger,
		}
	}

	client, err := posthog.NewWithConfig(apiKey, phc)
	if err != nil {
		return nil, err
	}

	c := &EventReporter{
		o11y:           observability.NewObserver(name, logger, o.tracerProvider),
		client:         client,
		eventCounter:   eventCounter,
		errorCounter:   errorCounter,
		circuitBreaker: circuitBreaker,
	}

	return c, nil
}

// breakerCallback bridges the PostHog client's asynchronous delivery outcomes to
// the circuit breaker. Enqueue only buffers the event, so the breaker must be
// driven from the background flush's Success/Failure callbacks to reflect real
// delivery health.
type breakerCallback struct {
	circuitBreaker circuitbreaking.CircuitBreaker
	errorCounter   metrics.Int64Counter
	logger         logging.Logger
}

func (cb *breakerCallback) Success(posthog.APIMessage) {
	if cb.circuitBreaker != nil {
		cb.circuitBreaker.Succeeded()
	}
}

func (cb *breakerCallback) Failure(_ posthog.APIMessage, err error) {
	cb.errorCounter.Add(context.Background(), 1)
	if cb.circuitBreaker != nil {
		cb.circuitBreaker.Failed()
	}
	cb.logger.Error("posthog event delivery failed", err)
}

// Close wraps the internal client's Close method.
func (c *EventReporter) Close(ctx context.Context) error {
	_, op := c.o11y.Begin(ctx)
	defer op.End()

	// The error is returned rather than only logged: this is the final flush of
	// whatever is still buffered, and a caller shutting down is the last chance
	// anyone has to notice those events did not make it.
	if err := c.client.Close(); err != nil {
		return observability.PrepareError(err, op.Span(), "closing connection")
	}

	return nil
}

// AddUser upsert's a user's identity.
func (c *EventReporter) AddUser(ctx context.Context, userID string, properties map[string]any) error {
	ctx, op := c.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, userID),
		observability.WithValue(keys.LengthKey, len(properties)),
	)
	defer op.End()

	if c.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	props := posthog.NewProperties()
	for k, v := range properties {
		props.Set(k, v)
	}

	err := c.client.Enqueue(posthog.Identify{
		DistinctId: userID,
		Properties: props,
	})
	if err != nil {
		c.errorCounter.Add(ctx, 1)
		c.circuitBreaker.Failed()
		return op.Error(err, "enqueueing identify event")
	}

	c.eventCounter.Add(ctx, 1)
	// Delivery success is signaled asynchronously via the client callback, not by a
	// successful enqueue (which only buffers the event).
	return nil
}

// EventOccurred associates events with a user.
func (c *EventReporter) EventOccurred(ctx context.Context, event, userID string, properties map[string]any) error {
	ctx, op := c.o11y.Begin(ctx,
		observability.WithValue(keys.UserIDKey, userID),
		observability.WithValue("event", event),
		observability.WithValue(keys.LengthKey, len(properties)),
	)
	defer op.End()

	if c.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	props := posthog.NewProperties()
	for k, v := range properties {
		props.Set(k, v)
	}

	err := c.client.Enqueue(posthog.Capture{
		DistinctId: userID,
		Event:      event,
		Properties: props,
	})
	if err != nil {
		c.errorCounter.Add(ctx, 1)
		c.circuitBreaker.Failed()
		return op.Error(err, "enqueueing capture event")
	}

	c.eventCounter.Add(ctx, 1)
	// Delivery success is signaled asynchronously via the client callback, not by a
	// successful enqueue (which only buffers the event).
	return nil
}

// EventOccurredAnonymous records an event for an anonymous user.
func (c *EventReporter) EventOccurredAnonymous(ctx context.Context, event, anonymousID string, properties map[string]any) error {
	ctx, op := c.o11y.Begin(ctx,
		observability.WithValue("anonymous_id", anonymousID),
		observability.WithValue("event", event),
		observability.WithValue(keys.LengthKey, len(properties)),
	)
	defer op.End()

	if c.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	props := posthog.NewProperties()
	for k, v := range properties {
		props.Set(k, v)
	}

	err := c.client.Enqueue(posthog.Capture{
		DistinctId: anonymousID,
		Event:      event,
		Properties: props,
	})
	if err != nil {
		c.errorCounter.Add(ctx, 1)
		c.circuitBreaker.Failed()
		return op.Error(err, "enqueueing capture event")
	}

	c.eventCounter.Add(ctx, 1)
	// Delivery success is signaled asynchronously via the client callback, not by a
	// successful enqueue (which only buffers the event).
	return nil
}
