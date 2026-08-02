package metering

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/cache"
	"github.com/primandproper/platform-go/v9/capitalism"
	"github.com/primandproper/platform-go/v9/clock"
	clockmock "github.com/primandproper/platform-go/v9/clock/mock"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/database/sqlite"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/metering/migrations"

	"github.com/shoenig/test/must"
)

// baseTime is the instant this suite works relative to. Deliberately mid-month
// and mid-day, so a period boundary is never coincidentally "now".
var baseTime = time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

// monthBounds is the calendar month baseTime falls in.
var monthBounds = Bounds{
	Start: time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
	End:   time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
}

// dayBounds is the calendar day baseTime falls in.
var dayBounds = Bounds{
	Start: time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC),
	End:   time.Date(2026, time.August, 16, 0, 0, 0, 0, time.UTC),
}

// errArbitrary stands in for any failure a dependency can produce.
var errArbitrary = platformerrors.New("the dependency is unreachable")

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

// stubClock is a manually advanced clock. Periods, staleness budgets, and flush
// backoff are all functions of elapsed time and these tests need months of it, so
// they control the clock rather than race the wall.
//
// A synctest bubble would normally spare us a double, but it advances fake time
// only once every goroutine in the bubble is durably blocked, and these tests
// drive a real SQLite file. Built on the generated mock so the methods nothing
// calls fail loudly instead of lying.
type stubClock struct {
	*clockmock.ClockMock

	now time.Time
	mu  sync.Mutex
}

var _ clock.Clock = (*stubClock)(nil)

func newStubClock() *stubClock {
	c := &stubClock{now: baseTime}

	c.ClockMock = &clockmock.ClockMock{
		NowFunc:       c.read,
		SinceFunc:     func(t time.Time) time.Duration { return c.read().Sub(t) },
		NewTickerFunc: clock.NewClock().NewTicker,
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

// prefixCounter names a fresh table pair per subtest. Subtests share one database
// and must not share tables — the flush claim predicate is global to the totals
// table, so one test's backlog would be another's.
var prefixCounter atomic.Uint64

// storeEnv is one live database plus the dialect to emit SQL for.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real SQL
// — placeholder rendering, the conflict clauses, the row-value IN lists, the
// partial indexes — without a container.
func newSQLiteEnv(t *testing.T) *storeEnv {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "metering.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newStore migrates a uniquely prefixed table pair and returns a Store over it.
func (e *storeEnv) newStore(t *testing.T) Store {
	t.Helper()

	store, _ := e.newStoreWithPrefix(t)

	return store
}

// newStoreWithPrefix is newStore, also handing back the prefix so a test can
// query the tables directly.
func (e *storeEnv) newStoreWithPrefix(t *testing.T) (store Store, prefix string) {
	t.Helper()

	prefix = fmt.Sprintf("mtr_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(t, err)
	must.SliceNotEmpty(t, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	store, err = NewSQLStore(e.client, WithTablePrefix(prefix))
	must.NoError(t, err)

	return store, prefix
}

// testMeter is the meter most of this suite counts.
const testMeter = "api_requests"

// testSubject is the account most of this suite is about.
const testSubject = "account-1"

// newTestRegistry builds a registry with one sum meter and one quota over it.
func newTestRegistry(t *testing.T, behavior QuotaBehavior, limit int64) *Registry {
	t.Helper()

	registry := NewRegistry()

	must.NoError(t, registry.RegisterMeter(Meter{
		Name:        testMeter,
		Unit:        "requests",
		Aggregation: AggregationSum,
		Period:      PeriodMonth,
	}))
	must.NoError(t, registry.RegisterQuota(Quota{
		Meter:    testMeter,
		Limit:    limit,
		Behavior: behavior,
		Period:   PeriodMonth,
	}))

	return registry
}

// newEntry builds an entry for the calendar month baseTime falls in.
func newEntry(key string, quantity int64, aggregation Aggregation) Entry {
	return newEntryAt(key, quantity, aggregation, baseTime)
}

// newEntryAt is newEntry with an explicit event time, for the ordering the last
// and max aggregations depend on.
func newEntryAt(key string, quantity int64, aggregation Aggregation, at time.Time) Entry {
	return Entry{
		Usage: Usage{
			Subject:        testSubject,
			Meter:          testMeter,
			Quantity:       quantity,
			IdempotencyKey: key,
			OccurredAt:     at,
		},
		Bounds:      monthBounds,
		Aggregation: aggregation,
	}
}

// stubCache is a cache.Cache whose expiry follows the stub clock.
//
// cache/memory reads the wall clock, so a staleness budget measured in seconds
// would either make these tests sleep or make them flaky. Only the four methods
// the enforcer uses are implemented; the rest report loudly rather than lying,
// because a silent no-op here would look like a cache that simply never hit.
type stubCache struct {
	clock   *stubClock
	entries map[string]stubCacheEntry

	mu sync.Mutex
}

type stubCacheEntry struct {
	expiresAt time.Time
	value     CachedTotal
}

var _ cache.Cache[CachedTotal] = (*stubCache)(nil)

func newStubCache(c *stubClock) *stubCache {
	return &stubCache{clock: c, entries: map[string]stubCacheEntry{}}
}

func (c *stubCache) Get(_ context.Context, key string) (*CachedTotal, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, cache.ErrNotFound
	}

	if !entry.expiresAt.IsZero() && !c.clock.read().Before(entry.expiresAt) {
		delete(c.entries, key)

		return nil, cache.ErrNotFound
	}

	value := entry.value

	return &value, nil
}

func (c *stubCache) Set(_ context.Context, key string, value *CachedTotal, opts ...cache.WriteOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := stubCacheEntry{value: *value}
	if expiry := cache.EffectiveExpiry(0, opts...); expiry > 0 {
		entry.expiresAt = c.clock.read().Add(expiry)
	}

	c.entries[key] = entry

	return nil
}

func (c *stubCache) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)

	return nil
}

func (c *stubCache) GetMany(context.Context, []string) (map[string]*CachedTotal, error) {
	return nil, platformerrors.New("stub cache does not implement GetMany")
}

func (c *stubCache) SetMany(context.Context, map[string]*CachedTotal, ...cache.WriteOption) error {
	return platformerrors.New("stub cache does not implement SetMany")
}

func (c *stubCache) DeleteMany(context.Context, []string) error {
	return platformerrors.New("stub cache does not implement DeleteMany")
}

func (c *stubCache) DeleteByPrefix(context.Context, string) error {
	return platformerrors.New("stub cache does not implement DeleteByPrefix")
}

func (c *stubCache) Flush(context.Context) error {
	return platformerrors.New("stub cache does not implement Flush")
}

func (c *stubCache) Ping(context.Context) error { return nil }

// recordingReporter is an in-process UsageReporter. It records what was posted so
// a test can assert the delta and the idempotency key, and can be made to fail or
// panic on demand.
type recordingReporter struct {
	err error

	posts []capitalism.UsageReportInput

	mu sync.Mutex

	panicNow bool
}

var _ capitalism.UsageReporter = (*recordingReporter)(nil)

func (r *recordingReporter) ReportUsage(_ context.Context, input *capitalism.UsageReportInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.panicNow {
		panic("the provider SDK dereferenced a nil pointer")
	}

	if r.err != nil {
		return r.err
	}

	r.posts = append(r.posts, *input)

	return nil
}

func (r *recordingReporter) recorded() []capitalism.UsageReportInput {
	r.mu.Lock()
	defer r.mu.Unlock()

	posts := make([]capitalism.UsageReportInput, len(r.posts))
	copy(posts, r.posts)

	return posts
}

// zeroMapper resolves every subject and meter to nothing at all, which the
// Flusher reads as "not billable" rather than as something to post.
func zeroMapper() ProviderMapper {
	return ProviderMapperFunc(func(context.Context, string, string) (ProviderRef, error) {
		return ProviderRef{}, nil
	})
}

// staticMapper resolves every subject and meter to one pair of provider handles.
func staticMapper(customerID string) ProviderMapper {
	return ProviderMapperFunc(func(_ context.Context, _, meter string) (ProviderRef, error) {
		return ProviderRef{CustomerID: customerID, MeterName: meter}, nil
	})
}

// failingTotalStore fails only the durable total read, so the enforcer's
// fail-open and fail-closed branches are both reachable without a broken
// database.
type failingTotalStore struct {
	Store
}

func (s *failingTotalStore) Total(context.Context, string, string, Bounds) (*Total, error) {
	return nil, errArbitrary
}

// failingConsumeStore fails only Consume.
type failingConsumeStore struct {
	Store
}

func (s *failingConsumeStore) Consume(context.Context, Entry, int64, QuotaBehavior, time.Time) (*Decision, error) {
	return nil, errArbitrary
}

// failingClaimStore fails every flush claim, so a pass's error path is reachable.
// Embedding the real Store means only the one method under test is a double.
type failingClaimStore struct {
	Store
}

func (s *failingClaimStore) ClaimFlushable(context.Context, time.Time, int, int, time.Time) ([]*Total, error) {
	return nil, errArbitrary
}

// failingReapStore fails only the retention reap, so a pass's other chores still
// run and the partial result is observable.
type failingReapStore struct {
	Store
}

func (s *failingReapStore) ReapEvents(context.Context, time.Time, int) (int64, error) {
	return 0, errArbitrary
}

// failingSettleStore fails only MarkFlushed, so the "provider has it and the row
// does not say so" path is reachable.
type failingSettleStore struct {
	Store
}

func (s *failingSettleStore) MarkFlushed(context.Context, *Total, int64, time.Time) error {
	return errArbitrary
}

// failingReleaseStore fails only ReleaseFlush, so the path where the lease is
// left to expire is reachable.
type failingReleaseStore struct {
	Store
}

func (s *failingReleaseStore) ReleaseFlush(context.Context, *Total, string, time.Time) error {
	return errArbitrary
}

// recordFailingStore fails every durable record, so a recorder's error path is
// reachable.
type recordFailingStore struct {
	Store
}

func (s *recordFailingStore) Record(context.Context, []Entry, time.Time) (RecordResult, error) {
	return RecordResult{}, errArbitrary
}

func (s *recordFailingStore) RecordTx(
	context.Context,
	database.SQLQueryExecutor,
	[]Entry,
	time.Time,
) (RecordResult, error) {
	return RecordResult{}, errArbitrary
}

// countRows counts rows in one of a store's tables.
func countRows(t *testing.T, env *storeEnv, table string) int {
	t.Helper()

	var count int
	must.NoError(t, env.client.Reader().
		QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))

	return count
}

// Close satisfies cache.Cache.
func (s *stubCache) Close() error { return nil }
