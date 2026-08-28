package webhooks

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/identifiers"
	"github.com/primandproper/platform-go/v13/tenancy"
	"github.com/primandproper/platform-go/v13/webhooks/migrations"

	"github.com/shoenig/test/must"
)

// testClientConfig is the minimum database.ClientConfig a SQLite client needs.
type testClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*testClientConfig)(nil)

func (c *testClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *testClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *testClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *testClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *testClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *testClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *testClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }

// The fixtures the delivery tests sign and verify against. They live here
// rather than beside a signing test because the scheme itself moved to
// cryptography/requestsigning; what is left in this package is a caller of it.
var (
	signingTime = time.Unix(1753900000, 0).UTC()
	testBody    = []byte(`{"id":"abc","amount":42}`)
)

// The scopes the suite registers endpoints and dispatches deliveries in.
// testScope is what a multi-tenant consumer passes; otherScope is the neighbor
// whose rows must never appear in testScope's answers.
var (
	testScope  = tenancy.Of("acct_1")
	otherScope = tenancy.Of("acct_2")
)

// prefixCounter names a fresh set of tables per subtest. Subtests may share one
// database, and they must not share tables — the claim predicate is global to
// the dispatches table, so one test's backlog would be another's.
var prefixCounter atomic.Uint64

// storeEnv is one live database plus the dialect to emit SQL for.
type storeEnv struct {
	client  database.Client
	fresh   func(t *testing.T) database.Client
	dialect dialect.Dialect
}

// clientFor returns the database one subtest's tables live in.
//
// The two servers hand back the shared client: a subtest's prefix keeps its
// rows away from every other subtest's, and that is all it needs. SQLite gets a
// database of its own, because there a prefix is not enough — DDL invalidates
// every prepared statement on the whole database, so one subtest creating its
// tables makes a parallel subtest's next read fail with "database schema has
// changed" whatever prefix either of them is using. Prefixes keep their rows
// apart; only a separate file keeps their schema changes apart.
func (e *storeEnv) clientFor(t *testing.T) database.Client {
	t.Helper()

	if e.fresh == nil {
		return e.client
	}

	return e.fresh(t)
}

// newStore migrates a uniquely prefixed set of webhook tables and returns a
// Store over them.
func (e *storeEnv) newStore(t *testing.T) Store {
	t.Helper()

	client, prefix := e.database(t)

	store, err := NewSQLStore(client, WithTablePrefix(prefix))
	must.NoError(t, err)

	return store
}

// database migrates a uniquely prefixed set of webhook tables and returns the
// database they are in along with the prefix, for a test that wants to build
// the store over them itself.
func (e *storeEnv) database(t *testing.T) (client database.Client, prefix string) {
	t.Helper()

	client = e.clientFor(t)

	return client, e.migrateOn(t, client)
}

// migrateOn renders a uniquely prefixed set of webhook tables in one database.
func (e *storeEnv) migrateOn(t *testing.T, client database.Client) string {
	t.Helper()

	prefix := fmt.Sprintf("wh_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(t, err)
	must.SliceNotEmpty(t, stmts)

	for _, stmt := range stmts {
		_, execErr := client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	return prefix
}

// clientOf reads the database a store is running against, for the helpers that
// have to open a transaction of their own around a Store method.
//
// It is an assertion rather than a parameter because the store and its database
// are one fact: a helper handed both could be handed a mismatched pair, and the
// symptom would be a transaction that sees none of the rows the test wrote.
func clientOf(t *testing.T, store Store) database.Client {
	t.Helper()

	backed, ok := store.(*SQLStore)
	must.True(t, ok, must.Sprintf("store is %T, want *SQLStore", store))

	return backed.client
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real
// SQL — placeholder rendering, the ordering predicate, the lease arithmetic,
// the join projections — without a container.
func newSQLiteEnv(t *testing.T) *storeEnv {
	t.Helper()

	return &storeEnv{
		client:  newSQLiteClient(t),
		fresh:   newSQLiteClient,
		dialect: dialect.SQLite,
	}
}

// newSQLiteClient opens one database, in a directory the test owns. Each call
// is a database of its own — see storeEnv.clientFor for why a subtest needs
// one.
func newSQLiteClient(t *testing.T) database.Client {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "webhooks.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

// registerEndpoint saves an endpoint in testScope, subscribed to the given
// events.
func registerEndpoint(t *testing.T, store Store, id string, events ...EventType) *Endpoint {
	t.Helper()

	return registerScopedEndpoint(t, store, testScope, id, events...)
}

// registerScopedEndpoint saves an endpoint in an explicit scope, for the cases
// that need two tenants in one table.
func registerScopedEndpoint(t *testing.T, store Store, scope tenancy.Scope, id string, events ...EventType) *Endpoint {
	t.Helper()

	endpoint := &Endpoint{
		ID:            id,
		Scope:         scope,
		URL:           "https://93.184.216.34/hooks/" + id,
		ContentType:   DefaultContentType,
		Secret:        Secret{Current: []byte("secret-" + id)},
		Subscriptions: SubscribeTo(events...),
	}

	must.NoError(t, store.SaveEndpoint(t.Context(), endpoint))

	return endpoint
}

// dispatchTo writes a delivery and fans it out to the named endpoints, the way
// Dispatch would.
func dispatchTo(t *testing.T, store Store, delivery *Delivery, at time.Time, endpointIDs ...string) *Delivery {
	t.Helper()

	if delivery.ID == "" {
		delivery.ID = identifiers.New()
	}

	if delivery.Scope.Validate() != nil {
		delivery.Scope = testScope
	}

	must.NoError(t, clientOf(t, store).WithTransaction(t.Context(), func(q database.Tx) error {
		return store.Enqueue(t.Context(), q, delivery, endpointIDs, at)
	}))

	return delivery
}

// claimAll claims with a generous limit and a lease that will not expire during
// the test.
func claimAll(t *testing.T, store Store, now time.Time) []ClaimedDispatch {
	t.Helper()

	claimed, err := store.Claim(t.Context(), now, 100, now.Add(time.Minute))
	must.NoError(t, err)

	return claimed
}

// endpointsFor resolves testScope's fan-out set through a transaction, as
// Dispatch does.
func endpointsFor(t *testing.T, store Store, eventType EventType) []*Endpoint {
	t.Helper()

	return scopedEndpointsFor(t, store, testScope, eventType)
}

// scopedEndpointsFor resolves one scope's fan-out set through a transaction.
func scopedEndpointsFor(t *testing.T, store Store, scope tenancy.Scope, eventType EventType) []*Endpoint {
	t.Helper()

	var endpoints []*Endpoint

	must.NoError(t, clientOf(t, store).WithTransaction(t.Context(), func(q database.Tx) error {
		var err error
		endpoints, err = store.EndpointsForEvent(t.Context(), q, scope, eventType)

		return err
	}))

	return endpoints
}

// dispatchIDs projects the dispatch IDs of a claimed batch.
func dispatchIDs(claimed []ClaimedDispatch) []string {
	ids := make([]string, 0, len(claimed))
	for i := range claimed {
		ids = append(ids, claimed[i].ID)
	}

	return ids
}

// endpointIDsOf projects the endpoint IDs of a claimed batch.
func endpointIDsOf(claimed []ClaimedDispatch) []string {
	ids := make([]string, 0, len(claimed))
	for i := range claimed {
		ids = append(ids, claimed[i].EndpointID)
	}

	return ids
}

// baseTime is the instant the store suite works relative to.
var baseTime = time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

// ctxFor is a small convenience so the suite reads the same in both harnesses.
func ctxFor(t *testing.T) context.Context {
	t.Helper()

	return t.Context()
}

// subscriptionFor finds an endpoint's subscription to one event type, failing
// the test when there is none. The suite reads a subscription's identity back
// constantly, and the alternative at each site is a loop.
func subscriptionFor(t *testing.T, endpoint *Endpoint, eventType EventType) Subscription {
	t.Helper()

	for i := range endpoint.Subscriptions {
		if endpoint.Subscriptions[i].EventType == eventType {
			return endpoint.Subscriptions[i]
		}
	}

	t.Fatalf("endpoint %q has no subscription to %q", endpoint.ID, eventType)

	return Subscription{}
}
