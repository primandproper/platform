package registry

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/sqlite"
	"github.com/primandproper/platform-go/v13/tenancy"
	"github.com/primandproper/platform-go/v13/uploads/registry/migrations"

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

// The tenants the suite registers objects in. testScope is what a multi-tenant
// consumer passes; otherScope is the neighbor whose rows must never appear in
// testScope's answers.
var (
	testScope  = tenancy.Of("tenant_1")
	otherScope = tenancy.Of("tenant_2")
)

// prefixCounter names a fresh table per subtest. Subtests share one database and
// must not share a table — a scope's object list is global within the table, so
// one test's objects would be another's.
var prefixCounter atomic.Uint64

// storeEnv is one live database plus the dialect to emit SQL for.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newStore migrates a uniquely prefixed registry table and returns a Store over
// it.
func (e *storeEnv) newStore(t *testing.T) *SQLStore {
	t.Helper()

	store, err := NewSQLStore(e.client, WithTablePrefix(e.migrate(t)))
	must.NoError(t, err)

	return store
}

// migrate renders a uniquely prefixed registry table and returns the prefix.
func (e *storeEnv) migrate(t *testing.T) string {
	t.Helper()

	prefix := fmt.Sprintf("ur_%d", prefixCounter.Add(1))

	stmts, err := migrations.Statements(e.dialect, prefix)
	must.NoError(t, err)
	must.SliceNotEmpty(t, stmts)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	return prefix
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real
// SQL — placeholder rendering, the cursor comparison, the two counts riding on
// the rows — without a container.
func newSQLiteEnv(t *testing.T) *storeEnv {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "registry.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newObject is the object the suite records, with the fields a caller has to
// supply and nothing else.
func newObject(scope tenancy.Scope, key, ownerID string) *Object {
	return &Object{
		Scope:       scope,
		Key:         key,
		ContentType: "image/png",
		OwnerID:     ownerID,
		Size:        1024,
	}
}
