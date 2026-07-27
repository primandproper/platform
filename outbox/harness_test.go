package outbox

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v7/clock"
	"github.com/primandproper/platform-go/v7/database"
	"github.com/primandproper/platform-go/v7/database/sqlite"
	loggingnoop "github.com/primandproper/platform-go/v7/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v7/observability/tracing/noop"
	"github.com/primandproper/platform-go/v7/outbox/migrations"

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

// stubClock is a manually advanced clock. The relay reads time at claim,
// publish, and failure, so tests that assert on backoff need to control it
// rather than race the wall clock.
type stubClock struct {
	now time.Time
	mu  sync.Mutex
}

var _ clock.Clock = (*stubClock)(nil)

func newStubClock() *stubClock {
	return &stubClock{now: time.Date(2026, time.July, 27, 12, 0, 0, 0, time.UTC)}
}

func (c *stubClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *stubClock) Since(t time.Time) time.Duration { return c.Now().Sub(t) }

func (c *stubClock) Sleep(context.Context, time.Duration) error { return nil }

func (c *stubClock) NewTicker(d time.Duration) clock.Ticker { return clock.NewClock().NewTicker(d) }

func (c *stubClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

// newTestClient builds a SQLite-backed database.Client with the outbox table
// already created. SQLite exercises the real SQL — placeholder rendering, the
// ordering predicate, the lease arithmetic — without a container.
func newTestClient(t *testing.T) database.Client {
	t.Helper()

	ctx := t.Context()

	client, err := sqlite.NewDatabaseClient(
		ctx,
		loggingnoop.NewLogger(),
		tracingnoop.NewTracerProvider(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "outbox.db")},
		nil,
	)
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	stmts, err := migrations.Statements(migrations.DialectSQLite, DefaultTableName)
	must.NoError(t, err)

	if len(stmts) == 0 {
		t.Fatal("no outbox DDL statements rendered")
	}

	for _, stmt := range stmts {
		_, execErr := client.Writer().ExecContext(ctx, stmt)
		must.NoError(t, execErr)
	}

	return client
}

// enqueue writes messages through a Writer inside a transaction, the way a
// caller would.
func enqueue(t *testing.T, client database.Client, w *Writer, msgs ...Message) {
	t.Helper()

	must.NoError(t, client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
		return w.Enqueue(t.Context(), q, msgs...)
	}))
}

// countRows returns the number of rows matching the supplied WHERE clause.
func countRows(t *testing.T, client database.Client, where string) int {
	t.Helper()

	var n int
	must.NoError(t, client.Reader().
		QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+DefaultTableName+" WHERE "+where).
		Scan(&n))

	return n
}
