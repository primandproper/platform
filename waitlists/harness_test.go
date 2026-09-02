package waitlists

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/clock"
	clockmock "github.com/primandproper/platform-go/v14/clock/mock"
	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/database/sqlite"
	"github.com/primandproper/platform-go/v14/tenancy"
	"github.com/primandproper/platform-go/v14/waitlists/migrations"

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
// and must not share tables: a contact is unique per list across live and
// archived rows alike, and a suite that reused one table set would have subtests
// colliding on contacts they each chose freely.
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

// testSubject is whose signups the subject-keyed reads are about.
var testSubject = Subject{Type: SubjectUser, ID: "user-1"}

// testNow is the instant the suite's clock starts at. Every closing time below
// is expressed relative to it, so a list is open or closed because the test said
// so rather than because the wall clock happened to agree.
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
		&testClientConfig{connectionString: filepath.Join(tb.TempDir(), "waitlists.db")})
	must.NoError(tb, err)
	tb.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newStore migrates a uniquely prefixed table set and returns a store over it,
// on a clock parked at testNow.
func (e *storeEnv) newStore(tb testing.TB, opts ...SQLStoreOption) *SQLStore {
	tb.Helper()

	store, _ := e.newStoreWithPrefix(tb, opts...)

	return store
}

// newStoreWithPrefix is newStore, also handing back the prefix so a test can
// reach the tables directly.
func (e *storeEnv) newStoreWithPrefix(tb testing.TB, opts ...SQLStoreOption) (store *SQLStore, prefix string) {
	tb.Helper()

	prefix = fmt.Sprintf("wl_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(tb, err)
	must.SliceNotEmpty(tb, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(tb.Context(), stmt)
		must.NoError(tb, execErr, must.Sprintf("executing %q", stmt))
	}

	base := []SQLStoreOption{WithTablePrefix(prefix), WithClock(newStubClock())}

	store, err = NewSQLStore(e.client, append(base, opts...)...)
	must.NoError(tb, err)

	return store, prefix
}

// openList is a list that is still taking signups at testNow.
func openList(name string) *List {
	return &List{
		Name:        name,
		Description: "early access to the beta",
		ClosesAt:    testNow.Add(30 * 24 * time.Hour),
	}
}

// closedList is a list whose closing time has already passed at testNow.
func closedList(name string) *List {
	return &List{Name: name, ClosesAt: testNow.Add(-time.Hour)}
}

// mustCreateList writes a list and fails the test if it will not go in.
func mustCreateList(tb testing.TB, store *SQLStore, scope tenancy.Scope, list *List) *List {
	tb.Helper()

	created, err := store.CreateList(tb.Context(), scope, list)
	must.NoError(tb, err)
	must.NotNil(tb, created)

	return created
}

// mustJoin adds a contact to a list and fails the test if it will not go in.
func mustJoin(tb testing.TB, store *SQLStore, scope tenancy.Scope, listID string, signup *Signup) *Signup {
	tb.Helper()

	joined, err := store.Join(tb.Context(), scope, listID, signup)
	must.NoError(tb, err)
	must.NotNil(tb, joined)

	return joined
}

// stubClock is a manually advanced clock parked at testNow.
//
// This package decides whether a list is open by comparing its closing time
// against whatever the clock reads, and stamps every transition from the same
// reading — so a suite racing the wall would be a suite whose lists close when
// the machine is slow. It is built on the generated mock, so a method nothing
// here calls panics rather than quietly answering.
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
