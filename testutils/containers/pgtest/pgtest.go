// Package pgtest provides the postgres testcontainer setup that every
// postgres-backed suite in this repo would otherwise hand-roll: start the
// container with the shared retry policy and wait strategy, open a pgx-backed
// *sql.DB against it, ping it, and tear all of it down afterwards.
//
// Callers describe the shape they want with Options and receive a live Instance
// inside a closure, so a test body says what it does with postgres and nothing
// about how postgres is stood up or torn down.
package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/testutils/containers"

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
	image        string
	database     string
	username     string
	password     string
	customizers  []testcontainers.ContainerCustomizer
	maxOpenConns int
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

// WithCustomizers appends testcontainers customizers to the ones Run already
// applies. They run after the defaults, so they can override the wait strategy.
func WithCustomizers(customizers ...testcontainers.ContainerCustomizer) Option {
	return func(o *options) { o.customizers = append(o.customizers, customizers...) }
}

func newOptions(opts []Option) *options {
	cfg := &options{
		image:    DefaultImage,
		database: defaultCredential,
		username: defaultCredential,
		password: defaultCredential,
	}
	for _, opt := range opts {
		opt(cfg)
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
	// Host, MappedPort, Exec or a snapshot. Its lifecycle is not yours to manage.
	Container *postgrescontainer.PostgresContainer

	// ConnectionString is the DSN DB was opened with.
	ConnectionString string

	// Database, Username and Password are the credentials the container was
	// provisioned with, exposed so tests can reconnect or grant against them.
	Database string
	Username string
	Password string
}

// ConnectionStringFor builds a DSN for this container under different
// credentials, for suites that connect as a role they created rather than as
// the provisioning superuser.
func (i *Instance) ConnectionStringFor(tb testing.TB, database, username, password string) string {
	tb.Helper()

	ctx := tb.Context()

	host, err := i.Container.Host(ctx)
	must.NoError(tb, err)

	port, err := i.Container.MappedPort(ctx, "5432/tcp")
	must.NoError(tb, err)

	return fmt.Sprintf("postgres://%s:%s@%s/%s", username, password, net.JoinHostPort(host, port.Port()), database)
}

// Open opens and pings an additional pool against this container and closes it
// when the test ends. Use it alongside ConnectionStringFor to connect as another
// role; for the provisioning role, DB is already open.
func (i *Instance) Open(tb testing.TB, connectionString string) *sql.DB {
	tb.Helper()

	db, err := sql.Open(DriverName, connectionString)
	must.NoError(tb, err)
	must.NotNil(tb, db)

	tb.Cleanup(func() { closePool(tb, db) })

	containers.PingUntilReady(tb, tb.Context(), db.PingContext)

	return db
}

// Run starts a postgres container, opens a pool against it, and hands both to fn
// as an Instance. It is containers.Run with the postgres-shaped setup — image,
// credentials, readiness wait, sql.Open, ping — already applied, so the closure
// starts from a database it can query.
//
// As with containers.Run, the RUN_CONTAINER_TESTS gate is enforced here (the test
// skips without a Docker daemon), startup failures fail the test, and teardown of
// both the pool and the container is registered with tb.Cleanup — so fn is free to
// spawn parallel subtests against the Instance and return before they run.
func Run(tb testing.TB, fn func(ctx context.Context, pg *Instance), opts ...Option) {
	tb.Helper()

	if fn == nil {
		tb.Fatal("pgtest: Run requires a non-nil fn")
	}

	cfg := newOptions(opts)

	containers.Run(tb,
		func(ctx context.Context) (*postgrescontainer.PostgresContainer, error) {
			return postgrescontainer.Run(ctx, cfg.image, cfg.containerOptions()...)
		},
		func(ctx context.Context, container *postgrescontainer.PostgresContainer) {
			connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
			must.NoError(tb, err)

			db, err := sql.Open(DriverName, connectionString)
			must.NoError(tb, err)
			must.NotNil(tb, db)

			tb.Cleanup(func() { closePool(tb, db) })

			if cfg.maxOpenConns > 0 {
				db.SetMaxOpenConns(cfg.maxOpenConns)
			}

			containers.PingUntilReady(tb, ctx, db.PingContext)

			fn(ctx, &Instance{
				DB:               db,
				Container:        container,
				ConnectionString: connectionString,
				Database:         cfg.database,
				Username:         cfg.username,
				Password:         cfg.password,
			})
		},
	)
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
	return append([]testcontainers.ContainerCustomizer{
		postgrescontainer.WithDatabase(o.database),
		postgrescontainer.WithUsername(o.username),
		postgrescontainer.WithPassword(o.password),
		testcontainers.WithWaitStrategyAndDeadline(
			startupDeadline,
			wait.ForLog(readyLog).WithOccurrence(readyLogOccurence),
		),
	}, o.customizers...)
}
