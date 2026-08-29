package issuereports

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/clock"
	clockmock "github.com/primandproper/platform-go/v13/clock/mock"
	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/issuereports/migrations"
	"github.com/primandproper/platform-go/v13/tenancy"

	"github.com/shoenig/test/must"
)

// baseTime is the instant this suite works relative to. Deliberately mid-month
// and mid-day, so a boundary is never coincidentally "now".
var baseTime = time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)

// The two directories this suite works in. Two rather than one because half of
// what these tests assert is that a read in one cannot see the other's rows.
var (
	testScope  = tenancy.Of("acct_1")
	otherScope = tenancy.Of("acct_2")
)

// The two people. A report belongs to a scope and was filed by somebody inside
// it, and the reporter is what the privacy path keys on.
const (
	testReporter  = "user_1"
	otherReporter = "user_2"
)

// errCounterUnavailable stands in for a metrics provider that cannot build an
// instrument.
var errCounterUnavailable = platformerrors.New("the instrument is unavailable")

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

// stubClock is a manually advanced clock. closed_at is the one stamp this store
// writes from a clock, and the tests that care about it need a known distance
// between the filing and the resolution rather than whatever the wall did.
//
// Built on the generated mock so the methods nothing calls fail loudly instead
// of lying.
type stubClock struct {
	*clockmock.ClockMock

	now time.Time
	mu  sync.Mutex
}

var _ clock.Clock = (*stubClock)(nil)

func newStubClock() *stubClock {
	c := &stubClock{now: baseTime}

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

// prefixCounter names a fresh table per subtest. Subtests share one database and
// must not share a table: a scope's whole list is keyed on the scope and nothing
// else, so one subtest's queue would be another's.
var prefixCounter atomic.Uint64

// storeEnv is one live database plus the dialect its statements are generated
// for.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real SQL
// — placeholder rendering, the partial indexes, the guarded update — without a
// container.
func newSQLiteEnv(tb testing.TB) *storeEnv {
	tb.Helper()

	client, err := sqlite.NewDatabaseClient(tb.Context(),
		&testClientConfig{connectionString: filepath.Join(tb.TempDir(), "issuereports.db")})
	must.NoError(tb, err)
	tb.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newStore migrates a uniquely prefixed table and returns a store over it.
func (e *storeEnv) newStore(tb testing.TB, opts ...SQLStoreOption) *SQLStore {
	tb.Helper()

	store, _ := e.newStoreWithPrefix(tb, opts...)

	return store
}

// newStoreWithPrefix is newStore, also handing back the prefix so a test can
// query the table directly.
func (e *storeEnv) newStoreWithPrefix(tb testing.TB, opts ...SQLStoreOption) (store *SQLStore, prefix string) {
	tb.Helper()

	prefix = fmt.Sprintf("ir_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(tb, err)
	must.SliceNotEmpty(tb, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(tb.Context(), stmt)
		must.NoError(tb, execErr, must.Sprintf("executing %q", stmt))
	}

	store, err = NewSQLStore(e.client, append([]SQLStoreOption{WithTablePrefix(prefix)}, opts...)...)
	must.NoError(tb, err)

	return store, prefix
}

// newReport is one row's worth of input, with everything the store requires
// filled in.
func newReport(reporter, kind, details string) *Report {
	return &Report{
		Scope:       testScope,
		Reporter:    reporter,
		Kind:        kind,
		Details:     details,
		SubjectType: "recipes",
		SubjectID:   "recipe_1",
	}
}

// filed creates a report and returns it, for the tests whose subject is what
// happens next rather than the creation.
func filed(tb testing.TB, store *SQLStore, report *Report) *Report {
	tb.Helper()

	must.NoError(tb, store.CreateReport(tb.Context(), report))

	return report
}
