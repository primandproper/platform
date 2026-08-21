package writes_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/database/sqlite"
	"github.com/primandproper/platform-go/v12/database/writes"

	"github.com/shoenig/test/must"
)

// testClientConfig is the minimum database.ClientConfig a client needs.
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

// schema is a row a write can create and an append-only table a hook can write
// into. The pair is the whole point of this package: the second is durable
// exactly when the first is.
const schema = `
CREATE TABLE IF NOT EXISTS widgets (
    id TEXT NOT NULL PRIMARY KEY,
    name TEXT NOT NULL,
    archived_at DATETIME
);

CREATE TABLE IF NOT EXISTS audit_log (
    id TEXT NOT NULL PRIMARY KEY,
    resource TEXT NOT NULL,
    row_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    scope TEXT NOT NULL
);
`

// newClient builds a client over a fresh database file.
//
// SQLite is a real server that parses the real statements without a container,
// which is what these tests need: a rollback that actually rolls back, and a
// hook's INSERT landing in the same transaction as the row it describes.
func newClient(t *testing.T) database.Client {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "writes.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	for _, statement := range dialect.SplitStatements(schema) {
		_, execErr := client.Writer().ExecContext(t.Context(), statement)
		must.NoError(t, execErr)
	}

	return client
}

// newWriter builds a SQLite-backed writer along with the changes its hook saw.
func newWriter(t *testing.T, opts ...writes.Option) (database.Client, *writes.Writer, *changeLog) {
	t.Helper()

	client := newClient(t)
	seen := &changeLog{}

	writer, err := writes.New(client, append([]writes.Option{writes.WithHook(seen.record)}, opts...)...)
	must.NoError(t, err)

	return client, writer, seen
}

// insertWidget is a stand-in for a generated querier's create.
func insertWidget(ctx context.Context, exec database.SQLQueryExecutor, id, name string) error {
	_, err := exec.ExecContext(ctx, "INSERT INTO widgets (id, name) VALUES (?, ?)", id, name)

	return err
}

// countRows answers "did it commit" without going through anything under test.
func countRows(t *testing.T, client database.Client, table string) int {
	t.Helper()

	var count int

	must.NoError(t, client.Reader().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))

	return count
}

// changeLog collects what the hooks were told, so a test can assert on the
// account a write gave of itself.
//
// It is guarded because a hook runs wherever the write does, and subtests write
// in parallel.
type changeLog struct {
	changes []writes.Change
	mu      sync.Mutex
}

func (l *changeLog) record(_ context.Context, _ database.SQLQueryExecutor, change *writes.Change) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.changes = append(l.changes, *change)

	return nil
}

func (l *changeLog) all() []writes.Change {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.changes)
}
