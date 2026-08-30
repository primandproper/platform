package billing

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/billing/migrations"
	"github.com/primandproper/platform-go/v13/capitalism"
	"github.com/primandproper/platform-go/v13/clock"
	clockmock "github.com/primandproper/platform-go/v13/clock/mock"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/tenancy"

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

// prefixCounter names a fresh table set per subtest. Subtests share one database
// and must not share tables: a provider identifier is unique per scope across
// live and archived rows alike, and a suite that reused one table set would have
// subtests colliding on identifiers they each chose freely.
var prefixCounter atomic.Uint64

// The scope most of this suite works in, and a second one every isolation
// assertion reads through. Neither is global, because tenancy.Global() is the
// scope a bug defaults to — a predicate that lost its binding matches it — so a
// suite that worked entirely in it would pass with the scope dropped from every
// statement.
var (
	testScope  = tenancy.Of("acme")
	otherScope = tenancy.Of("other")
)

// The accounts this suite bills. Two, so that every account-keyed read has
// something it must not return.
const (
	testAccount  = "account-1"
	otherAccount = "account-2"
)

// testNow is the instant the suite's clock starts at. Every subscription period
// below is expressed relative to it, so an agreement is current or lapsed because
// the test said so rather than because the wall clock happened to agree.
//
// It is well clear of both horizons the schema has to survive: the SQLite
// lexicographic comparison over a text column wants a four-digit year, and the
// MySQL DATETIME range starts in 1000.
var testNow = time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)

// storeEnv is one live database plus the dialect it speaks.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real SQL
// — placeholder rendering, the guarded updates' predicates, the partial indexes
// — without a container.
func newSQLiteEnv(tb testing.TB) *storeEnv {
	tb.Helper()

	client, err := sqlite.NewDatabaseClient(tb.Context(),
		&testClientConfig{connectionString: filepath.Join(tb.TempDir(), "billing.db")})
	must.NoError(tb, err)
	tb.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newStore migrates a uniquely prefixed table set and returns a store over it, on
// a clock parked at testNow.
func (e *storeEnv) newStore(tb testing.TB, opts ...SQLStoreOption) *SQLStore {
	tb.Helper()

	store, _ := e.newStoreWithClock(tb, opts...)

	return store
}

// newStoreWithClock is newStore, also handing back the clock so a test can move
// time past a subscription's period end.
func (e *storeEnv) newStoreWithClock(tb testing.TB, opts ...SQLStoreOption) (*SQLStore, *stubClock) {
	tb.Helper()

	prefix := fmt.Sprintf("bl_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(tb, err)
	must.SliceNotEmpty(tb, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(tb.Context(), stmt)
		must.NoError(tb, execErr, must.Sprintf("executing %q", stmt))
	}

	stub := newStubClock()

	base := []SQLStoreOption{WithTablePrefix(prefix), WithClock(stub)}

	store, err := NewSQLStore(e.client, append(base, opts...)...)
	must.NoError(tb, err)

	return store, stub
}

// recurringProduct is a subscription product priced in whole dollars.
func recurringProduct(name string) *Product {
	return &Product{
		Name:                  name,
		Description:           "the paid tier",
		Kind:                  KindRecurring,
		AmountCents:           2_500,
		Currency:              "USD",
		BillingIntervalMonths: 1,
	}
}

// oneTimeProduct is a product sold once.
func oneTimeProduct(name string) *Product {
	return &Product{
		Name:        name,
		Kind:        KindOneTime,
		AmountCents: 999,
		Currency:    "USD",
	}
}

// currentSubscription is an agreement whose paid period covers testNow.
func currentSubscription(productID, accountID string) *Subscription {
	return &Subscription{
		BelongsToAccount:   accountID,
		ProductID:          productID,
		Status:             capitalism.SubscriptionStatusActive,
		CurrentPeriodStart: testNow.Add(-24 * time.Hour),
		CurrentPeriodEnd:   testNow.Add(30 * 24 * time.Hour),
	}
}

// lapsedSubscription is an agreement whose paid period ended before testNow.
func lapsedSubscription(productID, accountID string) *Subscription {
	return &Subscription{
		BelongsToAccount:   accountID,
		ProductID:          productID,
		Status:             capitalism.SubscriptionStatusCanceled,
		CurrentPeriodStart: testNow.Add(-60 * 24 * time.Hour),
		CurrentPeriodEnd:   testNow.Add(-30 * 24 * time.Hour),
	}
}

// outstandingPurchase is a sale whose money has not arrived.
func outstandingPurchase(productID, accountID string) *Purchase {
	return &Purchase{
		BelongsToAccount: accountID,
		ProductID:        productID,
		AmountCents:      999,
		Currency:         "USD",
	}
}

// pendingTransaction is a ledger row for an attempt still in flight.
func pendingTransaction(accountID string) *Transaction {
	return &Transaction{
		BelongsToAccount: accountID,
		Status:           TransactionPending,
		AmountCents:      999,
		Currency:         "USD",
	}
}

// mustCreateProduct writes a product and fails the test if it will not go in.
func mustCreateProduct(tb testing.TB, store *SQLStore, scope tenancy.Scope, product *Product) *Product {
	tb.Helper()

	created, err := store.CreateProduct(tb.Context(), scope, product)
	must.NoError(tb, err)
	must.NotNil(tb, created)

	return created
}

// mustCreateSubscription writes a subscription and fails the test if it will not
// go in.
func mustCreateSubscription(
	tb testing.TB,
	store *SQLStore,
	scope tenancy.Scope,
	subscription *Subscription,
) *Subscription {
	tb.Helper()

	created, err := store.CreateSubscription(tb.Context(), scope, subscription)
	must.NoError(tb, err)
	must.NotNil(tb, created)

	return created
}

// mustCreatePurchase writes a purchase and fails the test if it will not go in.
func mustCreatePurchase(tb testing.TB, store *SQLStore, scope tenancy.Scope, purchase *Purchase) *Purchase {
	tb.Helper()

	created, err := store.CreatePurchase(tb.Context(), scope, purchase)
	must.NoError(tb, err)
	must.NotNil(tb, created)

	return created
}

// mustRecordTransaction writes a ledger row and fails the test if it will not go
// in.
func mustRecordTransaction(
	tb testing.TB,
	store *SQLStore,
	scope tenancy.Scope,
	transaction *Transaction,
) *Transaction {
	tb.Helper()

	recorded, err := store.RecordTransaction(tb.Context(), scope, transaction)
	must.NoError(tb, err)
	must.NotNil(tb, recorded)

	return recorded
}

// stubClock is a manually advanced clock parked at testNow.
//
// This package decides which subscriptions are current by comparing their paid
// period against whatever the clock reads — so a suite racing the wall would be a
// suite whose agreements lapse when the machine is slow. It is built on the
// generated mock, so a method nothing here calls panics rather than quietly
// answering.
type stubClock struct {
	*clockmock.ClockMock

	now time.Time

	mu sync.Mutex
}

var _ clock.Clock = (*stubClock)(nil)

func newStubClock() *stubClock {
	c := &stubClock{now: testNow}

	c.ClockMock = &clockmock.ClockMock{
		NowFunc:   c.read,
		SinceFunc: func(t time.Time) time.Duration { return c.read().Sub(t) },
	}

	return c
}

func (c *stubClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *stubClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}
