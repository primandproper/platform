package settings

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/database/sqlite"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/pointer"
	"github.com/primandproper/platform-go/v14/settings/migrations"
	"github.com/primandproper/platform-go/v14/tenancy"

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
// and must not share tables: a definition's name is unique per scope, and a
// suite that reused one table set would have subtests colliding on names they
// each chose freely.
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

// testSubject is whose settings most of this suite is about.
var testSubject = Subject{Type: SubjectUser, ID: "user-1"}

// errCompanionWrite stands in for the write a consumer makes beside a setting —
// the audit entry, the outbox event — failing after the setting itself is in the
// transaction.
var errCompanionWrite = platformerrors.New("the companion write failed")

// storeEnv is one live database plus the dialect it speaks.
type storeEnv struct {
	client  database.Client
	dialect dialect.Dialect
}

// newSQLiteEnv builds a SQLite-backed environment. SQLite exercises the real SQL
// — placeholder rendering, the upsert's conflict clause, the batched read's
// placeholder expansion, the partial indexes — without a container.
func newSQLiteEnv(tb testing.TB) *storeEnv {
	tb.Helper()

	client, err := sqlite.NewDatabaseClient(tb.Context(),
		&testClientConfig{connectionString: filepath.Join(tb.TempDir(), "settings.db")})
	must.NoError(tb, err)
	tb.Cleanup(func() { _ = client.Close() })

	return &storeEnv{client: client, dialect: dialect.SQLite}
}

// newStore migrates a uniquely prefixed table set and returns a store over it.
func (e *storeEnv) newStore(tb testing.TB, opts ...SQLStoreOption) *SQLStore {
	tb.Helper()

	store, _ := e.newStoreWithPrefix(tb, opts...)

	return store
}

// newStoreWithPrefix is newStore, also handing back the prefix so a test can
// reach the tables directly.
func (e *storeEnv) newStoreWithPrefix(tb testing.TB, opts ...SQLStoreOption) (store *SQLStore, prefix string) {
	tb.Helper()

	prefix = fmt.Sprintf("st_%d", prefixCounter.Add(1))

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

// The definitions this suite writes. Each is a function rather than a var
// because CreateDefinition fills in the id and the creation time, and a shared
// value would carry one subtest's row into the next.

func stringDefinition(name string) *Definition {
	return &Definition{
		Name:        name,
		Description: "how often a digest is sent",
		Kind:        KindString,
		Default:     pointer.To("weekly"),
		Enumeration: []string{"weekly", "daily", "never"},
	}
}

func boolDefinition(name string) *Definition {
	return &Definition{Name: name, Kind: KindBool, Default: pointer.To("true")}
}

func intDefinition(name string) *Definition {
	return &Definition{Name: name, Kind: KindInt}
}

// mustCreate writes a definition and fails the test if it will not go in.
func mustCreate(tb testing.TB, store *SQLStore, scope tenancy.Scope, definition *Definition) *Definition {
	tb.Helper()

	created, err := store.CreateDefinition(tb.Context(), scope, definition)
	must.NoError(tb, err)
	must.NotNil(tb, created)

	return created
}
