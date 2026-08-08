package webhooks

import (
	"encoding/json"
	"slices"
	"time"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// serviceName names the loggers, spans, and metrics this package emits.
const serviceName = "webhooks"

// Observability keys for this package's spans and log fields. Declared once so
// a field set on a span and the same field logged beside it cannot drift, and
// so the webhooks. prefix is applied uniformly — an un-namespaced attribute
// name collides with every other component writing to the same trace.
const (
	endpointIDKey   = "webhooks.endpoint_id"
	endpointURLKey  = "webhooks.endpoint_url"
	deliveryIDKey   = "webhooks.delivery_id"
	dispatchIDKey   = "webhooks.dispatch_id"
	eventTypeKey    = "webhooks.event_type"
	orderingKeyKey  = "webhooks.ordering_key"
	attemptsKey     = "webhooks.attempts"
	fanoutKey       = "webhooks.fanout"
	claimedKey      = "webhooks.claimed"
	statusCodeKey   = "webhooks.status_code"
	circuitOpenKey  = "webhooks.circuit_open"
	backlogDepthKey = "webhooks.backlog_depth"
	backlogAgeKey   = "webhooks.backlog_age_seconds"
	reapedKey       = "webhooks.reaped"
	replayedKey     = "webhooks.replayed"
)

var (
	// ErrUnknownEventType indicates an event type absent from the Catalog. It is
	// returned both when registering an endpoint that subscribes to it and when
	// dispatching it, so a typo cannot reach the wire from either direction.
	ErrUnknownEventType = platformerrors.New("unknown webhook event type")

	// ErrNoSigningSecret indicates an endpoint with no current signing secret.
	// Unsigned delivery is not an option this package offers: a subscriber that
	// cannot authenticate a payload cannot safely act on it.
	ErrNoSigningSecret = platformerrors.New("webhook endpoint has no signing secret")

	// ErrEndpointDisabled indicates a Replay targeting an endpoint that is
	// disabled. Dispatch skips disabled endpoints silently — that is what
	// disabling means — but an operator naming one explicitly is told why
	// nothing happened.
	ErrEndpointDisabled = platformerrors.New("webhook endpoint is disabled")

	// ErrDeliveryNotFound indicates a Replay naming a delivery/endpoint pair
	// that was never dispatched, or has since been reaped.
	ErrDeliveryNotFound = platformerrors.New("webhook delivery not found")

	// ErrNilStore indicates a nil Store. It wraps errors.ErrNilInputParameter,
	// so a caller may check either.
	ErrNilStore = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil webhook store")

	// ErrNilExecutor indicates Dispatch was called without a query executor. It
	// wraps errors.ErrNilInputParameter, so a caller may check either.
	ErrNilExecutor = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil query executor")

	// ErrNilDelivery indicates Dispatch was called with no Delivery.
	ErrNilDelivery = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil webhook delivery")

	// ErrNilEndpoint indicates a nil Endpoint was passed for registration.
	ErrNilEndpoint = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil webhook endpoint")
)

// EventDefinition describes one subscribable event type. It is deliberately
// thin: the library needs to know an event type exists in order to reject a
// subscription to one that does not, and needs nothing else about it.
//
// Description is what an endpoint-management UI shows beside the checkbox, and
// the reason this is a struct rather than a set — a bare set would push every
// consumer into maintaining that text somewhere else, out of step with the
// events themselves.
type EventDefinition struct {
	// Description is human-facing prose explaining when the event fires.
	Description string `json:"description"`
}

// Catalog is the set of event types an application publishes, keyed by event
// type. It is supplied at construction rather than stored, because what an
// event means is an application opinion and the library has none.
//
// Subscribing to an event outside the catalog is rejected at registration, and
// dispatching one is rejected at Dispatch. Both matter: an event type is a
// string, strings are typo-prone, and a subscription to "reciped.created" that
// is accepted silently produces an endpoint that never fires and no signal
// explaining why.
type Catalog map[string]EventDefinition

// Known reports whether eventType is in the catalog.
func (c Catalog) Known(eventType string) bool {
	_, ok := c[eventType]

	return ok
}

// EventTypes returns the catalog's event types, sorted, for rendering a
// subscription UI or an API response.
func (c Catalog) EventTypes() []string {
	types := make([]string, 0, len(c))
	for eventType := range c {
		types = append(types, eventType)
	}

	slices.Sort(types)

	return types
}

// Secret carries an endpoint's HMAC signing keys.
//
// It is a pair rather than a single value so that rotation is not an outage.
// Every delivery is signed under Current and, while Previous is set, again
// under Previous; both signatures travel in the same header. A subscriber
// therefore accepts deliveries throughout the window in which it is switching
// keys, and the operator clears Previous once every subscriber has moved.
//
// A single per-account secret — which is what this package exists partly to
// replace — makes that impossible: rolling it breaks every subscriber for the
// account at the same instant, so in practice it never gets rolled.
type Secret struct {
	// Current is the key new signatures are minted under. Required.
	Current []byte `json:"-"`
	// Previous is an outgoing key still emitted alongside Current during a
	// rotation window. Empty outside one.
	Previous []byte `json:"-"`
}

// Endpoint is one subscriber: where deliveries go, what they are signed with,
// and which events reach it.
type Endpoint struct {
	// Headers are static headers added to every request to this endpoint, for
	// subscribers that need a routing token or a tenant hint. The signature,
	// timestamp, content type, and event headers this package sets are not
	// overridable from here — see reservedHeaders.
	Headers map[string]string `json:"headers,omitempty"`
	// ID identifies the endpoint. Generated at registration when empty.
	ID string `json:"id"`
	// URL is the absolute https:// URL deliveries are POSTed to.
	URL string `json:"url"`
	// ContentType is the request's Content-Type. Defaults to application/json.
	ContentType string `json:"contentType"`
	// Secret carries the signing keys. Never serialized: an endpoint travels
	// through API responses and logs, and its secret must not.
	Secret Secret `json:"-"`
	// Events are the catalog event types this endpoint subscribes to.
	Events []string `json:"events"`
	// Disabled stops delivery without deleting the endpoint or its history,
	// which is what an operator wants when a subscriber is misbehaving.
	Disabled bool `json:"disabled"`
}

// Delivery is one event to fan out. It is the application's unit; the
// per-endpoint unit it expands into is a dispatch, which callers do not
// construct.
type Delivery struct {
	// ID identifies the delivery, and is what Replay names. Generated when empty.
	ID string `json:"id"`
	// EventType is the catalog event type. Must be in the Catalog.
	EventType string `json:"eventType"`
	// OrderingKey groups deliveries that must arrive in order — typically the
	// subject resource's ID. Deliveries sharing a key reach a given endpoint in
	// the order they were dispatched; deliveries with different keys, or with
	// none, are unordered relative to each other.
	//
	// Ordering is per endpoint as well as per key. A subscriber that is timing
	// out delays only its own queue for that key, never another subscriber's.
	OrderingKey string `json:"orderingKey,omitempty"`
	// Payload is the event body, delivered to every subscriber byte for byte as
	// supplied. It is json.RawMessage rather than any so that what is signed and
	// what is sent are the same bytes — re-marshaling between signing and
	// sending is exactly how a signature comes to cover something other than the
	// request body.
	Payload json.RawMessage `json:"payload"`
}

// Attempt is one recorded HTTP attempt against one endpoint. Attempts are
// append-only and are the delivery log: what was tried, when, and what came
// back.
type Attempt struct {
	// AttemptedAt is when the request was issued.
	AttemptedAt time.Time `json:"attemptedAt"`
	// ID identifies the attempt.
	ID string `json:"id"`
	// DeliveryID is the delivery this attempted.
	DeliveryID string `json:"deliveryID"`
	// EndpointID is the endpoint it was sent to.
	EndpointID string `json:"endpointID"`
	// Error is the transport or status error, rendered. Empty on success. It is
	// a string because it is stored and read by a human, not re-wrapped.
	Error string `json:"error,omitempty"`
	// Duration is how long the request took.
	Duration time.Duration `json:"duration"`
	// StatusCode is the response status, or 0 if no response was received.
	StatusCode int `json:"statusCode"`
	// AttemptCount is which attempt this was, 1-indexed.
	AttemptCount int `json:"attemptCount"`
}

// Succeeded reports whether the attempt was accepted by the subscriber.
func (a *Attempt) Succeeded() bool {
	return a.Error == "" && successfulStatus(a.StatusCode)
}

// successfulStatus reports whether a response status counts as delivered.
//
// 2xx only. A 3xx is not success: the subscriber is asking for the request to
// be re-issued somewhere else, and following that redirect would deliver a
// signed payload to a host the operator never registered — so redirects are
// refused at the client and treated as failure here.
func successfulStatus(code int) bool {
	return code >= 200 && code < 300
}
