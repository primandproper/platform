// Package pgtest provides the postgres testcontainer setup that every
// postgres-backed suite in this repo would otherwise hand-roll: start the
// container with the shared retry policy and wait strategy, open a pgx-backed
// *sql.DB against it, ping it, and tear all of it down afterwards.
//
// Callers describe the shape they want with Options and receive a live Instance
// inside a closure, so a test body says what it does with postgres and nothing
// about how postgres is stood up or torn down.
//
// # One server, one isolated database per test
//
// A container per test gives perfect isolation at a price that stops scaling
// once a package has a few dozen: every test pays a container start plus a full
// migration replay, and a package running its tests in parallel asks the Docker
// daemon for that many postgres instances at once. Past a certain width the
// daemon stops answering and containers fail their readiness wait — not because
// anything is wrong with the test, but because nothing was rationing the daemon.
//
// Isolation does not require a container, though. Start one Instance per test
// binary and hand each test its own corner of it:
//
//   - Instance.Schema creates a private schema, injects it through the DSN's
//     search_path, and drops it on cleanup. Cheap, and available on managed
//     postgres where CREATE DATABASE is restricted.
//   - Instance.Template migrates one database, and Template.Clone copies it per
//     test with CREATE DATABASE ... TEMPLATE — a file copy rather than a replay
//     of every migration. Stronger isolation, since it covers extensions and
//     everything else schema-scoped rules do not, and no search_path to inject.
//
// Neither is strictly better. Schemas are cheaper and portable; clones isolate
// more and skip per-test migration entirely.
//
// # The lock key that makes schemas parallel
//
// Schema-isolated tests migrate concurrently, and they must not serialize on
// one advisory lock while doing it. Pass migrate.WithSchemaScopedLockKey() to
// the Migrator you hand to WithMigration: it derives the lock ID from the
// connection's current schema, so deployments on the default schema still share
// one lock and test schemas never contend with each other. Without it, parallel
// setup becomes a queue.
//
// Clones need no such thing. Postgres advisory locks are per-database, and
// migrations run once into the template before any test starts.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/testutils/containers"

	// The pgx stdlib driver is registered here so that callers get a working
	// "pgx" driver from importing pgtest alone.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shoenig/test/must"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	// DefaultImage is the postgres image Run launches when no override is given.
	DefaultImage = "postgres:17-alpine"

	// DriverName is the database/sql driver Instance.DB and Instance.Open use.
	DriverName = "pgx"

	// DefaultMaxConnections is the server-wide connection ceiling Run provisions
	// the container with, replacing postgres' default of 100.
	//
	// One container now serves every test in a binary — Schema and Clone hand out
	// pools against one server rather than one server each — so the ceiling is
	// spent by the whole run at once instead of per test. At the default it is
	// whichever test connects last that fails, with "too many clients already",
	// which reads as flake rather than as the budget it is.
	DefaultMaxConnections = 200

	defaultCredential = "platformtest"

	// A cold start on a busy CI host has to cover an image pull plus initdb, and
	// postgres logs its readiness line twice — once for the bootstrap server that
	// runs the init scripts, once for the real one.
	startupDeadline   = 2 * time.Minute
	readyLog          = "database system is ready to accept connections"
	readyLogOccurence = 2
)

// Option configures Run.
type Option func(*options)

type options struct {
	image          string
	database       string
	username       string
	password       string
	dsnEnvVar      string
	customizers    []testcontainers.ContainerCustomizer
	maxOpenConns   int
	maxConnections int
	required       bool
}

// WithImage overrides DefaultImage. Use it for postgres derivatives that the
// rest of this setup still applies to, e.g. "pgvector/pgvector:pg17".
func WithImage(image string) Option {
	return func(o *options) { o.image = image }
}

// WithCredentials overrides the database name, superuser and password the
// container is provisioned with. Tests that create or drop roles want distinct
// credentials so they cannot collide with the identifiers under test.
func WithCredentials(database, username, password string) Option {
	return func(o *options) {
		o.database, o.username, o.password = database, username, password
	}
}

// WithMaxOpenConns caps Instance.DB's pool. Set it well above the number of
// concurrent subtests sharing an Instance, otherwise they starve each other.
// Zero (the default) leaves database/sql's unlimited default in place.
func WithMaxOpenConns(n int) Option {
	return func(o *options) { o.maxOpenConns = n }
}

// WithMaxConnections overrides DefaultMaxConnections, the server-wide ceiling
// the container is started with. Raise it for a binary whose tests are both
// numerous and parallel; the budget is one number shared by every pool Run,
// Schema and Clone hand out, so it has to cover the widest moment of the run
// rather than the widest single test. Zero leaves the image's own default.
func WithMaxConnections(n int) Option {
	return func(o *options) { o.maxConnections = n }
}

// WithRequiredPostgres makes an unavailable postgres a test failure instead of
// a skip, by way of containers.Required.
//
// The default gate is right for this module and wrong for a service: a library
// whose consumers may have no Docker daemon should skip, while a service whose
// postgres backend is only ever exercised here should fail loudly, because a
// skip is indistinguishable from a pass and a backend can reach zero coverage
// that way without anyone noticing. -short still skips either way.
func WithRequiredPostgres() Option {
	return func(o *options) { o.required = true }
}

// WithDSNFromEnv names an environment variable holding a postgres DSN. When it
// is set and non-empty, Run connects to that server and starts no container at
// all — the first rung of the resolution ladder, ahead of -short and ahead of
// starting anything.
//
// It is how a suite runs against a postgres that CI already provides, and how a
// developer points the whole binary at a local server. The container-only
// fields of Instance are absent on this path: Container is nil, and Database,
// Username and Password are read out of the DSN rather than from
// WithCredentials.
func WithDSNFromEnv(name string) Option {
	return func(o *options) { o.dsnEnvVar = name }
}

// WithCustomizers appends testcontainers customizers to the ones Run already
// applies. They run after the defaults, so they can override the wait strategy.
func WithCustomizers(customizers ...testcontainers.ContainerCustomizer) Option {
	return func(o *options) { o.customizers = append(o.customizers, customizers...) }
}

func newOptions(opts []Option) *options {
	cfg := &options{
		image:          DefaultImage,
		database:       defaultCredential,
		username:       defaultCredential,
		password:       defaultCredential,
		maxConnections: DefaultMaxConnections,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// Instance is the live postgres handed to a Run closure. DB covers the common
// case; the remaining fields are there for the tests that need a second
// connection, a different role, or the container API itself.
type Instance struct {
	// DB is an open, pinged pool against Database as Username.
	DB *sql.DB

	// Container is the underlying testcontainer, for the rare test that needs
	// Exec or a snapshot. Its lifecycle is not yours to manage. It is nil when
	// WithDSNFromEnv resolved the server, since there is no container then;
	// Host and Port are populated either way.
	Container *postgrescontainer.PostgresContainer

	// ConnectionString is the DSN DB was opened with.
	ConnectionString string

	// Host and Port locate the server DB is connected to, whether that is the
	// container or the server named by WithDSNFromEnv.
	Host string
	Port string

	// Database, Username and Password are the credentials the server was
	// reached with, exposed so tests can reconnect or grant against them.
	Database string
	Username string
	Password string
}

// ConnectionStringFor builds a DSN for this server under different credentials,
// for suites that connect as a role they created rather than as the
// provisioning superuser.
func (i *Instance) ConnectionStringFor(tb testing.TB, database, username, password string) string {
	tb.Helper()

	if i.Host == "" {
		tb.Fatal("pgtest: Instance has no host; it was not produced by Run")
	}

	return fmt.Sprintf("postgres://%s@%s/%s",
		url.UserPassword(username, password).String(),
		net.JoinHostPort(i.Host, i.Port),
		database,
	)
}

// Open opens and pings an additional pool against this container and closes it
// when the test ends. Use it alongside ConnectionStringFor to connect as another
// role; for the provisioning role, DB is already open.
func (i *Instance) Open(tb testing.TB, connectionString string) *sql.DB {
	tb.Helper()

	return openPool(tb, tb.Context(), connectionString, 0, 0)
}

// Run resolves a postgres, opens a pool against it, and hands both to fn as an
// Instance. It is containers.Run with the postgres-shaped setup — image,
// credentials, readiness wait, sql.Open, ping — already applied, so the closure
// starts from a database it can query.
//
// The resolution ladder, in order:
//
//  1. the DSN in the environment variable named by WithDSNFromEnv, if that
//     option was given and the variable is set. No container is started.
//  2. -short, which skips.
//  3. a container. Whether an unavailable one skips or fails is the suite's
//     call — see WithRequiredPostgres — and by default it skips, along with the
//     RUN_CONTAINER_TESTS gate.
//
// As with containers.Run, startup failures fail the test, and teardown of both
// the pool and the container is registered with tb.Cleanup — so fn is free to
// spawn parallel subtests against the Instance and return before they run.
//
// One Run per test binary is the shape this is built for. Give each test its
// own schema with Instance.Schema, or its own database with Instance.Template
// and Template.Clone, rather than a container each.
func Run(tb testing.TB, fn func(ctx context.Context, pg *Instance), opts ...Option) {
	tb.Helper()

	if fn == nil {
		tb.Fatal("pgtest: Run requires a non-nil fn")
	}

	cfg := newOptions(opts)

	if dsn := cfg.dsnFromEnv(); dsn != "" {
		runAgainstDSN(tb, cfg, dsn, fn)

		return
	}

	var runOpts []containers.RunOption
	if cfg.required {
		runOpts = append(runOpts, containers.Required())
	}

	containers.Run(tb,
		func(ctx context.Context) (*postgrescontainer.PostgresContainer, error) {
			return postgrescontainer.Run(ctx, cfg.image, cfg.containerOptions()...)
		},
		func(ctx context.Context, container *postgrescontainer.PostgresContainer) {
			connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
			must.NoError(tb, err)

			host, err := container.Host(ctx)
			must.NoError(tb, err)

			port, err := container.MappedPort(ctx, "5432/tcp")
			must.NoError(tb, err)

			fn(ctx, &Instance{
				DB:               openPool(tb, ctx, connectionString, cfg.maxOpenConns, 0),
				Container:        container,
				ConnectionString: connectionString,
				Host:             host,
				Port:             port.Port(),
				Database:         cfg.database,
				Username:         cfg.username,
				Password:         cfg.password,
			})
		},
		runOpts...,
	)
}

// dsnFromEnv reads the first rung of the resolution ladder, or "" when the
// caller named no variable or the one they named is unset.
func (o *options) dsnFromEnv() string {
	if o.dsnEnvVar == "" {
		return ""
	}

	return strings.TrimSpace(os.Getenv(o.dsnEnvVar))
}

// runAgainstDSN is the WithDSNFromEnv path: a server somebody else is running,
// so there is nothing to start, nothing to gate on and nothing to terminate.
// -short is still honored, because a caller asking for a fast answer does not
// want a database round-trip either.
func runAgainstDSN(tb testing.TB, cfg *options, dsn string, fn func(ctx context.Context, pg *Instance)) {
	tb.Helper()

	if testing.Short() {
		tb.SkipNow()
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		tb.Fatalf("pgtest: parsing DSN from %s: %v", cfg.dsnEnvVar, err)
	}

	password, _ := parsed.User.Password()
	ctx := tb.Context()

	fn(ctx, &Instance{
		DB:               openPool(tb, ctx, dsn, cfg.maxOpenConns, 0),
		ConnectionString: dsn,
		Host:             parsed.Hostname(),
		Port:             parsed.Port(),
		Database:         strings.TrimPrefix(parsed.Path, "/"),
		Username:         parsed.User.Username(),
		Password:         password,
	})
}

// openPool opens, sizes, pings and registers teardown for a pool. Non-positive
// sizes leave database/sql's own defaults in place.
func openPool(tb testing.TB, ctx context.Context, connectionString string, maxOpen, maxIdle int) *sql.DB {
	tb.Helper()

	db, err := sql.Open(DriverName, connectionString)
	must.NoError(tb, err)
	must.NotNil(tb, db)

	tb.Cleanup(func() { closePool(tb, db) })

	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if maxIdle > 0 {
		db.SetMaxIdleConns(maxIdle)
	}

	containers.PingUntilReady(tb, ctx, db.PingContext)

	return db
}

// closePool drains a pool at the end of a test, logging rather than failing if it
// cannot: by then the test's own assertions have already had their say.
func closePool(tb testing.TB, db *sql.DB) {
	tb.Helper()

	if err := db.Close(); err != nil {
		tb.Logf("pgtest: closing pool: %v", err)
	}
}

// containerOptions renders the resolved options as testcontainers customizers.
// User-supplied customizers come last so they can override the defaults.
func (o *options) containerOptions() []testcontainers.ContainerCustomizer {
	defaults := []testcontainers.ContainerCustomizer{
		postgrescontainer.WithDatabase(o.database),
		postgrescontainer.WithUsername(o.username),
		postgrescontainer.WithPassword(o.password),
		testcontainers.WithWaitStrategyAndDeadline(
			startupDeadline,
			wait.ForLog(readyLog).WithOccurrence(readyLogOccurence),
		),
	}

	// Appended to the module's own `postgres -c fsync=off` rather than replacing
	// it, so the image keeps whatever else it wants on the command line.
	if o.maxConnections > 0 {
		defaults = append(defaults, testcontainers.WithCmdArgs("-c", fmt.Sprintf("max_connections=%d", o.maxConnections)))
	}

	return append(defaults, o.customizers...)
}
