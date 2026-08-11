package webhooks

import (
	"testing"
	"time"

	clockmock "github.com/primandproper/platform-go/v10/clock/mock"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/filtering"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	metricsmock "github.com/primandproper/platform-go/v10/observability/metrics/mock"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

// bogusDialectClient reports a dialect this package cannot emit SQL for.
//
// The unsupported-dialect branch is otherwise unreachable: the dialect comes
// from the client rather than the caller, and every client this module ships
// reports one of the three supported dialects. Only the embedded Dialect is
// consulted before the constructor gives up, so the embedded Client is never
// called.
type bogusDialectClient struct {
	database.Client
}

func (bogusDialectClient) Dialect() dialect.Dialect { return "oracle" }

func TestNewSQLStore(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		store, err := NewSQLStore(env.client)
		must.NoError(t, err)
		test.NotNil(t, store)
	})

	T.Run("unsupported dialect", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		_, err := NewSQLStore(bogusDialectClient{env.client})
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewSQLStore(nil)
		test.ErrorIs(t, err, ErrNilDatabaseClient)
	})

	// The prefix is interpolated into query text, not bound.
	T.Run("rejects a prefix that is not an identifier", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		_, err := NewSQLStore(env.client, WithTablePrefix("webhook; DROP TABLE users"))
		test.ErrorIs(t, err, dialect.ErrInvalidIdentifier)
	})
}

// TestSQLStore_SQLite runs the behavioral suite against SQLite. The same suite
// runs against real Postgres and MySQL in containers_test.go.
func TestSQLStore_SQLite(T *testing.T) {
	T.Parallel()

	runStoreSuite(T, newSQLiteEnv(T))
}

// runStoreSuite is the behavioral contract every dialect owes.
func runStoreSuite(t *testing.T, env *storeEnv) {
	t.Helper()

	t.Run("saves and reads back an endpoint", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		saved := &Endpoint{
			ID:          "endpoint-1",
			URL:         "https://93.184.216.34/hooks",
			ContentType: "application/cloudevents+json",
			Secret:      Secret{Current: []byte("current"), Previous: []byte("previous")},
			Headers:     map[string]string{"X-Tenant": "acme"},
			Events:      []string{"order.created", "order.updated"},
		}
		must.NoError(t, store.SaveEndpoint(ctxFor(t), saved))

		got, err := store.GetEndpoint(ctxFor(t), "endpoint-1")
		must.NoError(t, err)

		test.EqOp(t, saved.URL, got.URL)
		test.EqOp(t, saved.ContentType, got.ContentType)
		test.Eq(t, []byte("current"), got.Secret.Current)
		test.Eq(t, []byte("previous"), got.Secret.Previous)
		test.Eq(t, map[string]string{"X-Tenant": "acme"}, got.Headers)
		test.Eq(t, []string{"order.created", "order.updated"}, got.Events)
		test.False(t, got.Disabled)
	})

	// "Not rotating" and "rotating to an empty key" must not be confusable in
	// the column, so an absent previous secret round-trips as absent.
	t.Run("an absent previous secret stays absent", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		got, err := store.GetEndpoint(ctxFor(t), "endpoint-1")
		must.NoError(t, err)

		test.SliceEmpty(t, got.Secret.Previous)
	})

	t.Run("re-saving replaces the subscription set", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		registerEndpoint(t, store, "endpoint-1", "order.created", "order.updated")
		registerEndpoint(t, store, "endpoint-1", "order.updated")

		got, err := store.GetEndpoint(ctxFor(t), "endpoint-1")
		must.NoError(t, err)

		test.Eq(t, []string{"order.updated"}, got.Events)

		// And the endpoint is no longer resolved for the event it dropped.
		test.SliceEmpty(t, endpointsFor(t, env, store, "order.created"))
		test.SliceLen(t, 1, endpointsFor(t, env, store, "order.updated"))
	})

	t.Run("resolves the fan-out set for an event", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		registerEndpoint(t, store, "endpoint-1", "order.created")
		registerEndpoint(t, store, "endpoint-2", "order.created", "order.updated")
		registerEndpoint(t, store, "endpoint-3", "order.updated")

		test.Eq(t, []string{"endpoint-1", "endpoint-2"}, idsOf(endpointsFor(t, env, store, "order.created")))
		test.Eq(t, []string{"endpoint-2", "endpoint-3"}, idsOf(endpointsFor(t, env, store, "order.updated")))
		test.SliceEmpty(t, endpointsFor(t, env, store, "order.deleted"))
	})

	// Excluded at fan-out rather than at delivery: a dispatch row for a disabled
	// endpoint would sit permanently undeliverable in the backlog.
	t.Run("excludes disabled and archived endpoints from fan-out", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		registerEndpoint(t, store, "live", "order.created")

		disabled := registerEndpoint(t, store, "disabled", "order.created")
		disabled.Disabled = true
		must.NoError(t, store.SaveEndpoint(ctxFor(t), disabled))

		registerEndpoint(t, store, "archived", "order.created")
		must.NoError(t, store.ArchiveEndpoint(ctxFor(t), "archived"))

		test.Eq(t, []string{"live"}, idsOf(endpointsFor(t, env, store, "order.created")))
	})

	t.Run("lists live endpoints", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		registerEndpoint(t, store, "endpoint-1", "order.created")
		registerEndpoint(t, store, "endpoint-2", "order.created")
		registerEndpoint(t, store, "endpoint-3", "order.created")
		must.NoError(t, store.ArchiveEndpoint(ctxFor(t), "endpoint-2"))

		listed, err := store.ListEndpoints(ctxFor(t), filtering.DefaultQueryFilter())
		must.NoError(t, err)

		test.SliceLen(t, 2, listed.Data)
		test.EqOp(t, uint64(2), listed.TotalCount)

		// Subscriptions come back with each row.
		for _, endpoint := range listed.Data {
			test.Eq(t, []string{"order.created"}, endpoint.Events)
		}
	})

	t.Run("fans a delivery out into one dispatch per endpoint", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		registerEndpoint(t, store, "endpoint-1", "order.created")
		registerEndpoint(t, store, "endpoint-2", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody},
			baseTime, "endpoint-1", "endpoint-2")

		claimed := claimAll(t, store, baseTime)

		must.SliceLen(t, 2, claimed)
		test.Eq(t, []string{"endpoint-1", "endpoint-2"}, endpointIDsOf(claimed))

		// The payload and the endpoint's secrets arrive with the claim, so the
		// worker makes one round trip per batch rather than one per dispatch.
		for i := range claimed {
			test.Eq(t, testBody, claimed[i].Payload)
			test.EqOp(t, "order.created", claimed[i].EventType)
			must.NotNil(t, claimed[i].Endpoint)
			test.SliceNotEmpty(t, claimed[i].Endpoint.Secret.Current)
			test.EqOp(t, 1, claimed[i].Attempts)
		}
	})

	t.Run("enqueuing to nobody writes nothing", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		must.NoError(t, env.client.WithTransaction(ctxFor(t), func(q database.SQLQueryExecutor) error {
			return store.Enqueue(ctxFor(t), q, &Delivery{ID: "d", EventType: "order.created", Payload: testBody}, nil, baseTime)
		}))

		test.SliceEmpty(t, claimAll(t, store, baseTime))
	})

	// The ordering guarantee. Two deliveries sharing a key reach an endpoint
	// oldest-first, and the second is not claimable until the first lands.
	t.Run("holds back a keyed dispatch behind an earlier one", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		first := dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody, OrderingKey: "order-7"},
			baseTime, "endpoint-1")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody, OrderingKey: "order-7"},
			baseTime.Add(time.Second), "endpoint-1")

		claimed := claimAll(t, store, baseTime.Add(time.Minute))
		must.SliceLen(t, 1, claimed)
		test.EqOp(t, first.ID, claimed[0].DeliveryID)

		// Still blocked while the first is in flight.
		test.SliceEmpty(t, claimAll(t, store, baseTime.Add(time.Minute)))

		must.NoError(t, store.MarkDelivered(ctxFor(t), claimed[0].ID, baseTime.Add(time.Minute)))

		next := claimAll(t, store, baseTime.Add(2*time.Minute))
		must.SliceLen(t, 1, next)
		test.NotEqOp(t, first.ID, next[0].DeliveryID)
	})

	// Ordering is per (endpoint, key), not per key. A subscriber that is stuck
	// must delay only its own queue — otherwise a dead endpoint stalls healthy
	// ones, which is the failure circuit breaking exists to prevent.
	t.Run("one endpoint's backlog does not block another's", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "slow", "order.created")
		registerEndpoint(t, store, "fast", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody, OrderingKey: "order-7"},
			baseTime, "slow", "fast")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody, OrderingKey: "order-7"},
			baseTime.Add(time.Second), "slow", "fast")

		first := claimAll(t, store, baseTime.Add(time.Minute))
		must.SliceLen(t, 2, first)

		// Only "fast" completes its first dispatch.
		for i := range first {
			if first[i].EndpointID == "fast" {
				must.NoError(t, store.MarkDelivered(ctxFor(t), first[i].ID, baseTime.Add(time.Minute)))
			}
		}

		// "fast" advances to its second delivery; "slow" is still held.
		//
		// Claimed inside "slow"'s lease (claimAll leases for a minute from the
		// claim instant), so what holds its second dispatch back is the ordering
		// predicate rather than an expired lease letting the first be reclaimed.
		second := claimAll(t, store, baseTime.Add(90*time.Second))
		must.SliceLen(t, 1, second)
		test.EqOp(t, "fast", second[0].EndpointID)
	})

	t.Run("unkeyed dispatches claim freely", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		for i := range 3 {
			dispatchTo(t, env, store,
				&Delivery{EventType: "order.created", Payload: testBody},
				baseTime.Add(time.Duration(i)*time.Second), "endpoint-1")
		}

		test.SliceLen(t, 3, claimAll(t, store, baseTime.Add(time.Minute)))
	})

	t.Run("a lease keeps a second claim away until it expires", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed, err := store.Claim(ctxFor(t), baseTime, 10, baseTime.Add(30*time.Second))
		must.NoError(t, err)
		must.SliceLen(t, 1, claimed)

		// Inside the lease.
		again, err := store.Claim(ctxFor(t), baseTime.Add(10*time.Second), 10, baseTime.Add(time.Minute))
		must.NoError(t, err)
		test.SliceEmpty(t, again)

		// Past it.
		reclaimed, err := store.Claim(ctxFor(t), baseTime.Add(31*time.Second), 10, baseTime.Add(2*time.Minute))
		must.NoError(t, err)
		must.SliceLen(t, 1, reclaimed)

		// The attempt count survived the reclaim, so a dispatch that reliably
		// kills its worker eventually dies rather than looping forever.
		test.EqOp(t, 2, reclaimed[0].Attempts)
	})

	t.Run("respects the batch limit", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		for i := range 5 {
			dispatchTo(t, env, store,
				&Delivery{EventType: "order.created", Payload: testBody},
				baseTime.Add(time.Duration(i)*time.Second), "endpoint-1")
		}

		claimed, err := store.Claim(ctxFor(t), baseTime.Add(time.Minute), 2, baseTime.Add(2*time.Minute))
		must.NoError(t, err)
		test.SliceLen(t, 2, claimed)
	})

	t.Run("schedules a retry and then goes dead", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)

		// Retry scheduled into the future: not claimable yet, claimable after.
		must.NoError(t, store.RecordFailure(ctxFor(t), claimed[0].ID, claimed[0].Attempts, baseTime.Add(5*time.Minute), "boom", false))

		test.SliceEmpty(t, claimAll(t, store, baseTime.Add(time.Minute)))
		test.SliceLen(t, 1, claimAll(t, store, baseTime.Add(6*time.Minute)))

		// Dead is terminal. Native boolean handling differs per dialect; this is
		// the assertion that catches a TINYINT(1) mismatch.
		must.NoError(t, store.RecordFailure(ctxFor(t), claimed[0].ID, claimed[0].Attempts, baseTime, "boom", true))
		test.SliceEmpty(t, claimAll(t, store, baseTime.Add(time.Hour)))
	})

	// RecordFailure writes the attempt count it is given rather than leaving the
	// one Claim incremented, so a caller can decline to charge an attempt for a
	// failure the subscriber never saw — an open circuit being the case that
	// matters. Without this an endpoint down for an hour silently drains the
	// budget of everything queued behind it.
	t.Run("persists the attempt count it is given", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)
		must.EqOp(t, 1, claimed[0].Attempts)

		// Hand back the count from before this claim incremented it.
		must.NoError(t, store.RecordFailure(ctxFor(t), claimed[0].ID, 0, baseTime, "circuit open", false))

		next := claimAll(t, store, baseTime.Add(time.Minute))
		must.SliceLen(t, 1, next)

		// Back to 1 rather than 2: the skipped delivery cost nothing.
		test.EqOp(t, 1, next[0].Attempts)
	})

	// A dead dispatch must not block the ordering key behind it forever.
	t.Run("a dead dispatch releases its ordering key", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody, OrderingKey: "order-7"},
			baseTime, "endpoint-1")
		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody, OrderingKey: "order-7"},
			baseTime.Add(time.Second), "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)

		must.NoError(t, store.RecordFailure(ctxFor(t), claimed[0].ID, claimed[0].Attempts, baseTime, "poison", true))

		next := claimAll(t, store, baseTime.Add(time.Minute))
		test.SliceLen(t, 1, next)
	})

	t.Run("records and lists attempts", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		delivery := dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		must.NoError(t, store.RecordAttempt(ctxFor(t), &Attempt{
			DeliveryID: delivery.ID, EndpointID: "endpoint-1",
			AttemptCount: 1, StatusCode: 500, Error: "boom",
			Duration: 250 * time.Millisecond, AttemptedAt: baseTime,
		}))
		must.NoError(t, store.RecordAttempt(ctxFor(t), &Attempt{
			DeliveryID: delivery.ID, EndpointID: "endpoint-1",
			AttemptCount: 2, StatusCode: 200,
			Duration: 120 * time.Millisecond, AttemptedAt: baseTime.Add(time.Minute),
		}))

		listed, err := store.ListAttempts(ctxFor(t), delivery.ID, filtering.DefaultQueryFilter())
		must.NoError(t, err)
		must.SliceLen(t, 2, listed.Data)
		test.EqOp(t, uint64(2), listed.TotalCount)

		first, second := listed.Data[0], listed.Data[1]

		test.EqOp(t, 1, first.AttemptCount)
		test.EqOp(t, 500, first.StatusCode)
		test.EqOp(t, "boom", first.Error)
		test.EqOp(t, 250*time.Millisecond, first.Duration)
		test.False(t, first.Succeeded())
		test.EqOp(t, baseTime.Unix(), first.AttemptedAt.Unix())

		test.EqOp(t, 2, second.AttemptCount)
		test.True(t, second.Succeeded())

		// An ID is generated when the caller does not supply one.
		test.NotEqOp(t, "", first.ID)
	})

	t.Run("replays a dead dispatch with a fresh budget", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		delivery := dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)
		must.NoError(t, store.RecordFailure(ctxFor(t), claimed[0].ID, claimed[0].Attempts, baseTime, "gave up", true))
		must.SliceEmpty(t, claimAll(t, store, baseTime.Add(time.Hour)))

		must.NoError(t, store.Requeue(ctxFor(t), delivery.ID, "endpoint-1", baseTime.Add(time.Hour)))

		replayed := claimAll(t, store, baseTime.Add(2*time.Hour))
		must.SliceLen(t, 1, replayed)

		// Reset, not continued — a dead dispatch replayed without a reset would
		// die again on its next attempt.
		test.EqOp(t, 1, replayed[0].Attempts)
	})

	t.Run("replays a delivered dispatch", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		delivery := dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)
		must.NoError(t, store.MarkDelivered(ctxFor(t), claimed[0].ID, baseTime))

		must.NoError(t, store.Requeue(ctxFor(t), delivery.ID, "endpoint-1", baseTime.Add(time.Hour)))
		test.SliceLen(t, 1, claimAll(t, store, baseTime.Add(2*time.Hour)))
	})

	t.Run("replaying an unknown pair reports it", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		err := store.Requeue(ctxFor(t), "no-such-delivery", "endpoint-1", baseTime)
		test.ErrorIs(t, err, ErrDeliveryNotFound)
	})

	t.Run("reports the backlog", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		depth, oldest, err := store.Backlog(ctxFor(t))
		must.NoError(t, err)
		test.EqOp(t, int64(0), depth)
		test.True(t, oldest.IsZero())

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")
		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime.Add(time.Hour), "endpoint-1")

		depth, oldest, err = store.Backlog(ctxFor(t))
		must.NoError(t, err)
		test.EqOp(t, int64(2), depth)
		test.EqOp(t, baseTime.Unix(), oldest.Unix())
	})

	// A permanently broken subscriber must not read as a permanently growing
	// backlog.
	t.Run("the backlog excludes dead dispatches", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)
		must.NoError(t, store.RecordFailure(ctxFor(t), claimed[0].ID, claimed[0].Attempts, baseTime, "poison", true))

		depth, _, err := store.Backlog(ctxFor(t))
		must.NoError(t, err)
		test.EqOp(t, int64(0), depth)
	})

	t.Run("reaps delivered dispatches and their history", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		delivery := dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)

		must.NoError(t, store.RecordAttempt(ctxFor(t), &Attempt{
			DeliveryID: delivery.ID, EndpointID: "endpoint-1",
			AttemptCount: 1, StatusCode: 200, AttemptedAt: baseTime,
		}))
		must.NoError(t, store.MarkDelivered(ctxFor(t), claimed[0].ID, baseTime))

		// Inside the retention window, nothing goes.
		reaped, err := store.Reap(ctxFor(t), baseTime.Add(-time.Hour), 100)
		must.NoError(t, err)
		test.EqOp(t, int64(0), reaped)

		reaped, err = store.Reap(ctxFor(t), baseTime.Add(time.Hour), 100)
		must.NoError(t, err)
		test.EqOp(t, int64(1), reaped)

		// The log goes with it, so a reaped delivery leaves nothing behind.
		listed, err := store.ListAttempts(ctxFor(t), delivery.ID, filtering.DefaultQueryFilter())
		must.NoError(t, err)
		test.SliceEmpty(t, listed.Data)
	})

	t.Run("the reaper leaves undelivered dispatches alone", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		reaped, err := store.Reap(ctxFor(t), baseTime.Add(time.Hour), 100)
		must.NoError(t, err)
		test.EqOp(t, int64(0), reaped)

		test.SliceLen(t, 1, claimAll(t, store, baseTime.Add(time.Hour)))
	})

	// The endpoint is read at claim time, not captured at dispatch time, so a
	// secret rotated in between signs under the current key.
	t.Run("a claim sees the endpoint's current secret", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		endpoint := registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		endpoint.Secret = Secret{Current: []byte("rotated"), Previous: []byte("secret-endpoint-1")}
		must.NoError(t, store.SaveEndpoint(ctxFor(t), endpoint))

		claimed := claimAll(t, store, baseTime)
		must.SliceLen(t, 1, claimed)
		test.Eq(t, []byte("rotated"), claimed[0].Endpoint.Secret.Current)
		test.Eq(t, []byte("secret-endpoint-1"), claimed[0].Endpoint.Secret.Previous)
	})

	// An endpoint disabled between fan-out and claim is not delivered to.
	t.Run("a claim skips a since-disabled endpoint", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		endpoint := registerEndpoint(t, store, "endpoint-1", "order.created")

		dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		endpoint.Disabled = true
		must.NoError(t, store.SaveEndpoint(ctxFor(t), endpoint))

		test.SliceEmpty(t, claimAll(t, store, baseTime))
	})

	// Cursor pagination, for both paged reads. The cursor branch is what a
	// second page actually exercises, and a first-page-only test never renders
	// it. It owes every dialect: it is the one query shape whose placeholder
	// index is computed from the argument count rather than written literally,
	// and SQLite and MySQL both use positional '?', where a numbering mistake
	// is invisible. Only Postgres's numbered $N can catch it.
	t.Run("pages endpoints with a cursor", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		for _, id := range []string{"endpoint-1", "endpoint-2", "endpoint-3"} {
			registerEndpoint(t, store, id, "order.created")
		}

		filter := filtering.DefaultQueryFilter()
		filter.MaxResponseSize = new(uint16(2))

		first, err := store.ListEndpoints(ctxFor(t), filter)
		must.NoError(t, err)
		must.SliceLen(t, 2, first.Data)
		test.EqOp(t, "endpoint-2", first.Cursor)

		filter.Cursor = &first.Cursor

		second, err := store.ListEndpoints(ctxFor(t), filter)
		must.NoError(t, err)
		must.SliceLen(t, 1, second.Data)
		test.EqOp(t, "endpoint-3", second.Data[0].ID)

		// The total is the whole set, not the page.
		test.EqOp(t, uint64(3), second.TotalCount)
	})

	t.Run("pages attempts with a cursor", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		delivery := dispatchTo(t, env, store,
			&Delivery{EventType: "order.created", Payload: testBody}, baseTime, "endpoint-1")

		for i := range 3 {
			must.NoError(t, store.RecordAttempt(ctxFor(t), &Attempt{
				DeliveryID: delivery.ID, EndpointID: "endpoint-1",
				AttemptCount: i + 1, StatusCode: 500,
				AttemptedAt: baseTime.Add(time.Duration(i) * time.Minute),
			}))
		}

		filter := filtering.DefaultQueryFilter()
		filter.MaxResponseSize = new(uint16(2))

		first, err := store.ListAttempts(ctxFor(t), delivery.ID, filter)
		must.NoError(t, err)
		must.SliceLen(t, 2, first.Data)

		filter.Cursor = &first.Cursor

		second, err := store.ListAttempts(ctxFor(t), delivery.ID, filter)
		must.NoError(t, err)
		test.SliceLen(t, 1, second.Data)
		test.EqOp(t, uint64(3), second.TotalCount)
	})

	// A nil filter is the common call and must not page to zero rows.
	t.Run("a nil filter uses the defaults", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)
		registerEndpoint(t, store, "endpoint-1", "order.created")

		listed, err := store.ListEndpoints(ctxFor(t), nil)
		must.NoError(t, err)
		test.SliceLen(t, 1, listed.Data)
	})

	t.Run("guards against a nil executor", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		_, err := store.EndpointsForEvent(ctxFor(t), nil, "order.created")
		test.ErrorIs(t, err, ErrNilExecutor)

		test.ErrorIs(t,
			store.Enqueue(ctxFor(t), nil, &Delivery{ID: "d"}, []string{"e"}, baseTime),
			ErrNilExecutor,
		)
	})

	t.Run("guards against nil inputs", func(t *testing.T) {
		t.Parallel()

		store := env.newStore(t)

		test.ErrorIs(t, store.SaveEndpoint(ctxFor(t), nil), ErrNilEndpoint)
		test.ErrorIs(t, store.RecordAttempt(ctxFor(t), nil), platformerrors.ErrNilInputParameter)

		must.NoError(t, env.client.WithTransaction(ctxFor(t), func(q database.SQLQueryExecutor) error {
			test.ErrorIs(t, store.Enqueue(ctxFor(t), q, nil, []string{"e"}, baseTime), ErrNilDelivery)

			return nil
		}))
	})
}

// idsOf projects endpoint IDs.
func idsOf(endpoints []*Endpoint) []string {
	ids := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		ids = append(ids, endpoint.ID)
	}

	return ids
}

// errStoreInstrument is what the failing provider returns for the one
// instrument the store registers.
var errStoreInstrument = platformerrors.New("instrument unavailable")

func TestNewSQLStore_InstrumentFailures(T *testing.T) {
	T.Parallel()

	T.Run("refuses to build without the unreported row count counter", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		provider := &metricsmock.ProviderMock{
			NewInt64CounterFunc: func(string, ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
				return nil, errStoreInstrument
			},
		}

		store, err := NewSQLStore(env.client, WithStoreMetricsProvider(provider))
		test.Nil(t, store)
		test.ErrorIs(t, err, errStoreInstrument)
	})
}

func TestSQLStore_UsesTheInjectedClock(T *testing.T) {
	T.Parallel()

	T.Run("endpoint timestamps come from the clock it was given", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)

		stamped := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)

		var asked int

		injected := &clockmock.ClockMock{
			NowFunc: func() time.Time {
				asked++

				return stamped
			},
		}

		prefix := env.migrate(t)

		store, err := NewSQLStore(env.client, WithTablePrefix(prefix), WithStoreClock(injected))
		must.NoError(t, err)

		must.NoError(t, store.SaveEndpoint(t.Context(), &Endpoint{
			ID:     "endpoint",
			URL:    "https://example.com/hook",
			Secret: Secret{Current: []byte("s3cr3t")},
			Events: []string{"user.created"},
		}))
		must.NoError(t, store.ArchiveEndpoint(t.Context(), "endpoint"))

		// Both writes stamp through the clock rather than through time.Now.
		test.EqOp(t, 2, asked)
	})
}
