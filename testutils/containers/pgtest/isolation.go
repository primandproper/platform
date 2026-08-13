package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v10/random"
	"github.com/primandproper/platform-go/v10/testutils/containers"

	"github.com/shoenig/test/must"
)

const (
	// DefaultIsolatedMaxOpenConns and DefaultIsolatedMaxIdleConns size the pool
	// Schema and Clone hand back. They are deliberately tiny: the connection
	// ceiling belongs to the whole run, not to one test, and a pool sized for a
	// service that owns its database is the wrong shape for a few dozen suites
	// sharing one server. What over-sizing looks like downstream is "too many
	// clients already" from whichever test connects last.
	DefaultIsolatedMaxOpenConns = 4
	DefaultIsolatedMaxIdleConns = 2

	// maxIdentifierLength is postgres' NAMEDATALEN-1. Identifiers past it are
	// truncated silently, so two long names can arrive at the same schema or
	// database without anything saying so.
	maxIdentifierLength = 63

	// testNameBudget is how much of a test's name survives into an identifier.
	// The rest of the budget goes to the prefix and the random suffix, which is
	// what actually keeps two tests apart.
	testNameBudget = 40

	// randomSuffixBytes is hex-encoded, so it costs twice this many characters.
	randomSuffixBytes = 6

	schemaPrefix   = "pgtest"
	templatePrefix = "tmpl"
	clonePrefix    = "clone"
)

// MigrateFunc applies a schema to a freshly created schema or database. It is
// exactly the shape of database.Migrator's Migrate method, so a *migrate.Migrator
// satisfies it as a method value:
//
//	m, err := migrate.New(dialect.Postgres, migrations, migrate.WithSchemaScopedLockKey())
//	must.NoError(t, err)
//	schema := pg.Schema(t, pgtest.WithMigration(m.Migrate))
//
// It is a parameter rather than an import because database/migrate's own tests
// use this package, and importing it back would close the cycle.
type MigrateFunc func(ctx context.Context, db *sql.DB) error

// IsolationOption configures Instance.Schema, Instance.Template and
// Template.Clone.
type IsolationOption func(*isolationOptions)

type isolationOptions struct {
	migrate      MigrateFunc
	maxOpenConns int
	maxIdleConns int
}

// WithMigration supplies the migration to run against the new schema or
// database. Absent, the schema or clone is handed back empty, which is what a
// test that creates its own tables wants.
//
// For a schema, build the Migrator with migrate.WithSchemaScopedLockKey() or
// parallel setup serializes on one advisory lock. For a template it does not
// matter: the migration runs once, before any clone exists.
func WithMigration(fn MigrateFunc) IsolationOption {
	return func(o *isolationOptions) { o.migrate = fn }
}

// WithPoolSize overrides DefaultIsolatedMaxOpenConns and
// DefaultIsolatedMaxIdleConns for this schema or clone. Non-positive values
// leave database/sql's own unlimited defaults in place, which for a suite
// sharing one server is rarely what you want.
func WithPoolSize(maxOpen, maxIdle int) IsolationOption {
	return func(o *isolationOptions) {
		o.maxOpenConns, o.maxIdleConns = maxOpen, maxIdle
	}
}

func newIsolationOptions(opts []IsolationOption) *isolationOptions {
	cfg := &isolationOptions{
		maxOpenConns: DefaultIsolatedMaxOpenConns,
		maxIdleConns: DefaultIsolatedMaxIdleConns,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	return cfg
}

// Isolated is one test's private corner of a shared server: a schema from
// Instance.Schema, or a database from Template.Clone. Either way DB is a live,
// migrated pool and the underlying object is dropped when the test ends.
type Isolated struct {
	// DB is an open, pinged, deliberately small pool. For a schema its
	// search_path names Name, so unqualified DDL and DML land inside it.
	DB *sql.DB

	// Name is the schema or database name, unique within the run. Tests that
	// need to reconnect, or to assert on catalog rows, need it.
	Name string

	// ConnectionString is the DSN DB was opened with.
	ConnectionString string
}

// Schema creates a private schema on this instance, opens a pool whose
// search_path points at it, runs WithMigration if one was given, and drops the
// schema when tb ends.
//
// Everything unqualified — the tables the migrations create, the rows the test
// writes, and goose's own version table — lands inside the schema, so two tests
// running in parallel against one server never see each other's data. Give each
// test its own Schema; sharing one puts them back in the same database they were
// trying to get out of.
//
// The migration must not serialize with its peers. See WithMigration.
func (i *Instance) Schema(tb testing.TB, opts ...IsolationOption) *Isolated {
	tb.Helper()

	cfg := newIsolationOptions(opts)
	ctx := tb.Context()
	name := isolationName(tb, schemaPrefix)

	i.exec(tb, ctx, fmt.Sprintf("CREATE SCHEMA %s", quoteIdentifier(name)))

	// Registered before the pool is opened so that Cleanup's LIFO order closes
	// the pool first: DROP SCHEMA waits behind anything still holding a lock in
	// it, and a test that failed mid-transaction is exactly that.
	tb.Cleanup(func() {
		i.dropSchema(tb, context.WithoutCancel(ctx), name)
	})

	connectionString := i.searchPathDSN(tb, name)
	db := openPool(tb, ctx, connectionString, cfg.maxOpenConns, cfg.maxIdleConns)

	if cfg.migrate != nil {
		must.NoError(tb, cfg.migrate(ctx, db))
	}

	return &Isolated{DB: db, Name: name, ConnectionString: connectionString}
}

// Template is a migrated database that Clone copies per test. Build one per
// binary, from Instance.Template.
type Template struct {
	instance *Instance

	// Name is the template database's name.
	Name string
}

// Template creates a database, runs WithMigration against it once, and returns
// a handle that Clone copies per test. The database is dropped when tb ends.
//
// The migration pool is closed before Template returns, and that is load-bearing
// rather than tidy: CREATE DATABASE ... TEMPLATE refuses to run while any session
// is attached to the template, so a pool left open would fail the first clone
// instead of this call.
func (i *Instance) Template(tb testing.TB, opts ...IsolationOption) *Template {
	tb.Helper()

	cfg := newIsolationOptions(opts)
	ctx := tb.Context()
	name := isolationName(tb, templatePrefix)

	// CREATE DATABASE cannot run inside a transaction, so this is deliberately a
	// bare Exec on the instance pool rather than anything guarded.
	i.exec(tb, ctx, fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(name)))

	tb.Cleanup(func() {
		i.dropDatabase(tb, context.WithoutCancel(ctx), name)
	})

	if cfg.migrate != nil {
		db, err := sql.Open(DriverName, i.databaseDSN(tb, name))
		must.NoError(tb, err)

		containers.PingUntilReady(tb, ctx, db.PingContext)

		migrateErr := cfg.migrate(ctx, db)

		// Closed here, not via tb.Cleanup: the first Clone runs long before tb
		// ends, and it cannot run at all while this session is attached. The
		// close comes before the assertion so a failed migration still detaches.
		closePool(tb, db)

		must.NoError(tb, migrateErr)
	}

	return &Template{instance: i, Name: name}
}

// Clone copies the template into a fresh database and hands back a pool over it,
// dropped when tb ends. The copy is a file copy rather than a replay of every
// migration, which is what makes per-test isolation affordable.
//
// WithMigration is honored here too, for the occasional test that needs a
// migration the template does not carry; most callers migrate the template and
// pass nothing.
func (t *Template) Clone(tb testing.TB, opts ...IsolationOption) *Isolated {
	tb.Helper()

	cfg := newIsolationOptions(opts)
	ctx := tb.Context()
	name := isolationName(tb, clonePrefix)

	t.instance.exec(tb, ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s",
		quoteIdentifier(name), quoteIdentifier(t.Name)))

	tb.Cleanup(func() {
		t.instance.dropDatabase(tb, context.WithoutCancel(ctx), name)
	})

	connectionString := t.instance.databaseDSN(tb, name)
	db := openPool(tb, ctx, connectionString, cfg.maxOpenConns, cfg.maxIdleConns)

	if cfg.migrate != nil {
		must.NoError(tb, cfg.migrate(ctx, db))
	}

	return &Isolated{DB: db, Name: name, ConnectionString: connectionString}
}

// exec runs a statement on the instance pool, failing tb if it does not.
func (i *Instance) exec(tb testing.TB, ctx context.Context, query string) {
	tb.Helper()

	_, err := i.DB.ExecContext(ctx, query)
	must.NoError(tb, err)
}

// dropSchema and dropDatabase are the teardown half of Schema and Clone. Both
// log rather than fail: by cleanup time the test's own assertions have had their
// say, and a leftover object on a container about to be reaped is not worth
// turning a passing test red.
func (i *Instance) dropSchema(tb testing.TB, ctx context.Context, name string) {
	tb.Helper()

	if _, err := i.DB.ExecContext(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quoteIdentifier(name))); err != nil {
		tb.Logf("pgtest: dropping schema %s: %v", name, err)
	}
}

// dropDatabase drops a database out from under whatever is still connected to
// it. WITH (FORCE) terminates those sessions rather than erroring, which matters
// because a pool that logged its close failure is still holding a socket. It
// wants postgres 13 or newer, as every image this package defaults to is.
func (i *Instance) dropDatabase(tb testing.TB, ctx context.Context, name string) {
	tb.Helper()

	if _, err := i.DB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdentifier(name))); err != nil {
		tb.Logf("pgtest: dropping database %s: %v", name, err)
	}
}

// searchPathDSN renders this instance's DSN with search_path pointed at schema.
// pgx forwards unrecognized parameters as startup runtime parameters, so the
// setting arrives with the connection rather than as a statement some later
// connection in the pool might miss.
func (i *Instance) searchPathDSN(tb testing.TB, schema string) string {
	tb.Helper()

	return i.rewriteDSN(tb, func(u *url.URL) {
		query := u.Query()
		query.Set("search_path", schema)
		u.RawQuery = query.Encode()
	})
}

// databaseDSN renders this instance's DSN pointed at a different database.
func (i *Instance) databaseDSN(tb testing.TB, database string) string {
	tb.Helper()

	return i.rewriteDSN(tb, func(u *url.URL) { u.Path = "/" + database })
}

func (i *Instance) rewriteDSN(tb testing.TB, rewrite func(*url.URL)) string {
	tb.Helper()

	parsed, err := url.Parse(i.ConnectionString)
	must.NoError(tb, err)

	rewrite(parsed)

	return parsed.String()
}

// isolationName builds an identifier that is unique within a run and stays
// inside postgres' 63-byte limit. The test's name is in it for the human reading
// a failure; the random suffix is what makes it unique, since two long test
// names truncate to the same prefix.
func isolationName(tb testing.TB, prefix string) string {
	tb.Helper()

	suffix, err := random.GenerateHexEncodedString(context.WithoutCancel(tb.Context()), randomSuffixBytes)
	must.NoError(tb, err)

	name := fmt.Sprintf("%s_%s_%s", prefix, sanitizeIdentifier(tb.Name(), testNameBudget), suffix)
	if len(name) > maxIdentifierLength {
		// Unreachable with the budgets above, but truncating the *prefix* end
		// rather than letting postgres truncate the suffix end keeps the random
		// part, which is the part that has to survive.
		name = name[len(name)-maxIdentifierLength:]
	}

	return name
}

// sanitizeIdentifier reduces a test name to lowercase ASCII letters, digits and
// underscores, then trims it to budget. Subtest names carry slashes, spaces and
// whatever else a test author wrote in a t.Run label.
func sanitizeIdentifier(name string, budget int) string {
	var out strings.Builder

	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			out.WriteRune(r)
		default:
			out.WriteByte('_')
		}

		if out.Len() >= budget {
			break
		}
	}

	return strings.Trim(out.String(), "_")
}

// quoteIdentifier renders a name as a quoted postgres identifier. Everything
// this package generates is already safe, but the identifiers are interpolated
// into DDL that no placeholder can carry, so they are quoted rather than
// trusted.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
