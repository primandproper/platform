package webhooks

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/tenancy"
)

// Dispatch is one endpoint's copy of one delivery: the unit the worker actually
// retries, backs off, and gives up on.
//
// It exists as a distinct row from the Delivery because per-endpoint state is
// the whole point. A delivery that fanned out to five subscribers is not
// "failed" or "delivered" — four may have accepted it on the first attempt while
// the fifth is on its sixth retry, and a single status on the delivery cannot
// express that. Retrying at the delivery level would also redeliver to the four
// that already accepted it.
type Dispatch struct {
	// NextAttempt is when this dispatch next becomes claimable.
	NextAttempt time.Time `json:"nextAttempt"`
	// ID identifies the dispatch.
	ID string `json:"id"`
	// DeliveryID is the delivery being sent.
	DeliveryID string `json:"deliveryID"`
	// EndpointID is the subscriber it is being sent to.
	EndpointID string `json:"endpointID"`
	// OrderingKey is denormalized from the delivery so the claim predicate can
	// enforce ordering without joining. See the claim in
	// webhooks/internal/queries, which is where the guarantee is written down.
	OrderingKey string `json:"orderingKey,omitempty"`
	// LastError is the most recent failure, rendered.
	LastError string `json:"lastError,omitempty"`
	// Attempts is how many attempts have been made.
	Attempts int `json:"attempts"`
	// Dead marks a dispatch that exhausted its attempts. It is skipped by every
	// future claim and is what an operator replays.
	Dead bool `json:"dead"`
}

// ClaimedDispatch is a Dispatch the worker has leased, joined with everything
// needed to actually issue the request. It is assembled by the Store so the
// worker makes one round trip per batch rather than one per dispatch.
type ClaimedDispatch struct {
	// Endpoint is the subscriber, resolved at claim time rather than at
	// dispatch time — so a secret rotated between the event and its delivery
	// signs with the current key, and an endpoint disabled in between is not
	// delivered to at all.
	Endpoint *Endpoint `json:"endpoint"`
	// Payload is the delivery body, verbatim as dispatched.
	Payload []byte `json:"payload"`
	// Scope is the delivery's scope, read back from the delivery row.
	//
	// The worker does not filter on it — it delivers whatever it claimed, to the
	// endpoint the dispatch names, and the fan-out that produced the row already
	// resolved subscribers within this scope. It is here so that a delivery
	// failure, a dead dispatch, and a slow subscriber are attributable to a
	// tenant in the worker's logs and spans, which is otherwise the one place in
	// the pipeline where whose event it was has been forgotten.
	Scope tenancy.Scope `json:"scope"`
	// EventType is the delivery's event type.
	EventType EventType `json:"eventType"`

	Dispatch
}

// Store is the persistence seam.
//
// This package ships a SQL implementation (NewSQLStore) together with the DDL
// it needs (webhooks/migrations), so adopting webhooks does not mean writing
// this. The interface exists because delivery mechanics and persistence are
// genuinely separable, and an application with its own schema conventions —
// or one storing endpoints somewhere that is not a SQL database — should not
// have to fork the package to keep them.
//
// Methods taking a database.SQLQueryExecutor run inside the caller's
// transaction and must use it rather than a handle of their own; that is what
// makes Dispatch atomic with the state change that caused it. The rest own
// their own statements.
//
// Every method reaching an endpoint, a subscription, or a delivery takes a
// tenancy.Scope, or reads one off the value it was handed, and none of them
// offers an unscoped variant — an implementation must filter on it rather than
// treat it as a hint. A subscription carries no scope of its own; its owner is
// its endpoint's, and the scope is reached through that, the same way the
// delivery log reaches it through the delivery.
//
// The exceptions are the worker's own machinery below Enqueue: Claim, Backlog,
// and Reap deliberately span every scope, because one worker drains one queue
// for the whole deployment, and MarkDelivered, RecordFailure, RecordAttempt, and
// Requeue address a dispatch the worker or an operator is already holding. Those
// are the component servicing itself, not a consumer read.
type Store interface {
	// SaveEndpoint creates or replaces an endpoint, under the scope the endpoint
	// carries, and reconciles its subscriptions against the set it names.
	//
	// Reconciles rather than replaces: a subscription the endpoint already has is
	// kept, with its identity and its creation time; one it names for the first
	// time is created; one it no longer names is archived rather than deleted, so
	// that a subscription an endpoint has ended is still something the delivery
	// log can be read against. It fills the endpoint's Subscriptions with the rows
	// that are live afterwards, IDs included.
	SaveEndpoint(ctx context.Context, endpoint *Endpoint) error
	// GetEndpoint reads one of scope's endpoints, secrets included. It returns an
	// error wrapping database/sql.ErrNoRows when the endpoint does not exist —
	// including when it exists in another scope, which is the same answer as far
	// as this scope is concerned.
	GetEndpoint(ctx context.Context, scope tenancy.Scope, endpointID string) (*Endpoint, error)
	// ListEndpoints pages through the endpoints registered in scope.
	ListEndpoints(ctx context.Context, scope tenancy.Scope, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Endpoint], error)
	// ArchiveEndpoint retires one of scope's endpoints. Its delivery history is
	// retained: the attempts log outlives the endpoint, because "what did we send
	// them" is asked most often after someone has been removed.
	//
	// Its subscriptions are left as they are. An archived endpoint is excluded
	// from fan-out by its own archived_at, so archiving them too would buy
	// nothing and would lose which event types it was subscribed to if it is ever
	// re-registered.
	ArchiveEndpoint(ctx context.Context, scope tenancy.Scope, endpointID string) error

	// AddSubscription subscribes one of scope's endpoints to eventType and
	// returns the resulting row.
	//
	// It is idempotent on the (endpoint, event type) pair: subscribing to
	// something the endpoint already subscribes to returns the existing row, and
	// re-subscribing to something it archived revives that row rather than
	// minting a second one for the same pair. It returns an error wrapping
	// database/sql.ErrNoRows when the endpoint does not exist in scope.
	//
	// The catalog is not checked here — a Store has none. StoreDispatcher.Subscribe
	// is the entry point that gates on it, and it is the one an application should
	// call, for the reason Register exists rather than SaveEndpoint being public
	// API: an accepted subscription to an event type nothing publishes is an
	// endpoint that never fires and no signal explaining why.
	AddSubscription(ctx context.Context, scope tenancy.Scope, endpointID string, eventType EventType) (*Subscription, error)
	// GetSubscription reads one of scope's subscriptions, archived ones included —
	// "when did they stop receiving this" is a question about an archived row. It
	// returns an error wrapping database/sql.ErrNoRows when the subscription does
	// not exist under one of scope's endpoints.
	GetSubscription(ctx context.Context, scope tenancy.Scope, subscriptionID string) (*Subscription, error)
	// ListSubscriptions pages the live subscriptions of one of scope's endpoints.
	ListSubscriptions(ctx context.Context, scope tenancy.Scope, endpointID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Subscription], error)
	// ArchiveSubscription retires one of scope's subscriptions, so the endpoint
	// stops receiving that event type without its other subscriptions, its
	// delivery history, or its identity being touched.
	//
	// This is the method a flat event list cannot offer. Against one, "stop
	// sending me order.created" can only be expressed as a rewrite of the whole
	// set, which loses when it happened and races any concurrent edit of the same
	// endpoint.
	ArchiveSubscription(ctx context.Context, scope tenancy.Scope, subscriptionID string) error

	// EndpointsForEvent returns the enabled, unarchived endpoints in scope that
	// are subscribed to eventType, using the caller's executor.
	//
	// The scope is a parameter and not an option: this is the query whose missing
	// filter delivers one account's event to every other account's subscribers.
	EndpointsForEvent(ctx context.Context, q database.SQLQueryExecutor, scope tenancy.Scope, eventType EventType) ([]*Endpoint, error)
	// Enqueue writes a delivery and one dispatch per endpoint, in the caller's
	// transaction, so both commit with whatever else that transaction did.
	// The delivery's scope is stored with it.
	Enqueue(ctx context.Context, q database.Tx, delivery *Delivery, endpointIDs []string, now time.Time) error

	// Claim leases the next batch of due dispatches, incrementing their attempt
	// counts, and returns them ready to send.
	//
	// It spans every scope, deliberately: a worker delivers the whole
	// deployment's backlog, and a per-scope claim would need a list of scopes
	// nothing maintains. What it returns is scoped — each ClaimedDispatch says
	// which scope it came from.
	Claim(ctx context.Context, now time.Time, limit int, leaseUntil time.Time) ([]ClaimedDispatch, error)
	// MarkDelivered retires a dispatch that was accepted.
	MarkDelivered(ctx context.Context, dispatchID string, at time.Time) error
	// RecordFailure releases the lease, schedules the retry, and sets dead once
	// the dispatch has exhausted its attempts.
	//
	// attempts is persisted as given rather than left as Claim incremented it,
	// so the caller can decline to charge an attempt for a failure the
	// subscriber never saw — an open circuit being the case that matters.
	RecordFailure(ctx context.Context, dispatchID string, attempts int, nextAttempt time.Time, lastErr string, dead bool) error
	// RecordAttempt appends to the delivery log.
	RecordAttempt(ctx context.Context, attempt *Attempt) error
	// ListAttempts pages through the attempts recorded for one of scope's
	// deliveries. A delivery in another scope reads as one with no attempts,
	// which is what it is from here.
	ListAttempts(ctx context.Context, scope tenancy.Scope, deliveryID string, filter *filtering.QueryFilter) (*filtering.QueryFilteredResult[Attempt], error)

	// Requeue makes a delivery/endpoint pair claimable again, clearing its dead
	// flag and attempt count. It returns an error wrapping ErrDeliveryNotFound
	// if the pair was never dispatched or has been reaped.
	//
	// It takes no scope because the pair is already one: a dispatch exists only
	// where a delivery fanned out to an endpoint, and Dispatch resolves those
	// within one scope. StoreDispatcher.Replay is the scoped entry point, and it
	// establishes the scope by reading the endpoint in it first.
	Requeue(ctx context.Context, deliveryID, endpointID string, at time.Time) error

	// Backlog reports how many dispatches are waiting and when the oldest was
	// created, for the worker's health gauges. Like Claim, it spans every scope.
	Backlog(ctx context.Context) (depth int64, oldest time.Time, err error)
	// Reap deletes delivered dispatches, their deliveries, and their attempts
	// once they age past the retention window, up to limit rows.
	Reap(ctx context.Context, before time.Time, limit int) (int64, error)
}
