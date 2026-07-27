package outbox

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v7/database"
	"github.com/primandproper/platform-go/v7/database/mysql"
	"github.com/primandproper/platform-go/v7/database/postgres"
	platformerrors "github.com/primandproper/platform-go/v7/errors"
	loggingnoop "github.com/primandproper/platform-go/v7/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v7/observability/tracing/noop"
	"github.com/primandproper/platform-go/v7/outbox/migrations"
	"github.com/primandproper/platform-go/v7/testutils/containers"
	"github.com/primandproper/platform-go/v7/testutils/containers/pgtest"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/testcontainers/testcontainers-go"
	mysqlcontainers "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	defaultMySQLImage = "mariadb:11"

	// mysqlStartupDeadline mirrors the deadline the tableaccess suite uses; a
	// cold start has to cover an image pull plus initialization, and the
	// readiness line appears twice.
	mysqlStartupDeadline = 2 * time.Minute
)

// tableCounter names a fresh table per subtest. Subtests share one container,
// so they must not share a table — the claim predicate is global to the table
// and one test's backlog would be another's.
var tableCounter atomic.Uint64

// dialectEnv is one live database plus the dialect and claim mode the suite
// should exercise against it.
type dialectEnv struct {
	client    database.Client
	dialect   Dialect
	claimMode ClaimMode
}

// newTable creates a uniquely named outbox table and returns its name.
func (e *dialectEnv) newTable(t *testing.T) string {
	t.Helper()

	name := fmt.Sprintf("outbox_%d", tableCounter.Add(1))

	migrationDialect := migrations.Dialect(e.dialect)

	stmts, err := migrations.Statements(migrationDialect, name)
	must.NoError(t, err)

	for _, stmt := range stmts {
		_, execErr := e.client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	return name
}

// writer builds a Writer bound to the supplied table.
func (e *dialectEnv) writer(t *testing.T, c *stubClock, table string) *Writer {
	t.Helper()

	w, err := NewWriter(e.dialect, WithWriterClock(c), WithWriterTableName(table))
	must.NoError(t, err)

	return w
}

// relay builds a Relay bound to the supplied table.
func (e *dialectEnv) relay(t *testing.T, c *stubClock, table string) (*Relay, *recordingPublisher) {
	t.Helper()

	return newTestRelay(t, e.client, c, func(cfg *RelayConfig) {
		cfg.Dialect = e.dialect
		cfg.ClaimMode = e.claimMode
		cfg.TableName = table
	})
}

// countIn is countRows against an explicitly named table.
func countIn(t *testing.T, client database.Client, table, where string) int {
	t.Helper()

	var n int
	must.NoError(t, client.Reader().
		QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table+" WHERE "+where).
		Scan(&n))

	return n
}

// runDialectSuite is the behavioral contract every dialect owes. SQLite is
// covered by the in-process tests; this exists so the SQL that only a real
// server can validate — numbered placeholders, SKIP LOCKED, the correlated
// ordering subquery, MySQL's derived-table DELETE, native boolean and
// timestamp handling — is actually executed rather than merely rendered.
func runDialectSuite(t *testing.T, env *dialectEnv) {
	t.Helper()

	t.Run("publishes committed messages", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)
		relay, rec := env.relay(t, c, table)

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q,
				Message{Topic: "orders", Payload: map[string]any{"id": "a"}},
				Message{Topic: "orders", Payload: map[string]any{"id": "b"}},
			)
		}))

		relay.cycle(t.Context())

		test.SliceLen(t, 2, rec.payloads())
		test.EqOp(t, 0, countIn(t, env.client, table, "published_at IS NULL"))
	})

	t.Run("rolls back with the caller's transaction", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)

		boom := platformerrors.New("caller work failed")

		err := env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			if enqueueErr := w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}}); enqueueErr != nil {
				return enqueueErr
			}

			return boom
		})
		test.ErrorIs(t, err, boom)

		test.EqOp(t, 0, countIn(t, env.client, table, "1=1"))
	})

	t.Run("reschedules a failed publish and retries after the backoff", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)
		relay, rec := env.relay(t, c, table)

		rec.fail(platformerrors.New("broker down"))

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}})
		}))

		relay.cycle(t.Context())

		test.SliceEmpty(t, rec.payloads())
		test.EqOp(t, 1, countIn(t, env.client, table, "published_at IS NULL AND attempts = 1"))

		// Still inside the backoff window.
		relay.cycle(t.Context())
		test.EqOp(t, 1, countIn(t, env.client, table, "attempts = 1"))

		rec.fail(nil)
		c.advance(time.Minute)

		relay.cycle(t.Context())
		test.SliceLen(t, 1, rec.payloads())
		test.EqOp(t, 0, countIn(t, env.client, table, "published_at IS NULL"))
	})

	t.Run("quarantines a poison message without blocking the queue", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)
		relay, rec := env.relay(t, c, table)

		rec.fail(platformerrors.New("poison"))

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}})
		}))

		for range 3 {
			relay.cycle(t.Context())
			c.advance(time.Hour)
		}

		// Native boolean handling differs per dialect; this is the assertion
		// that catches a TINYINT(1) mismatch.
		test.EqOp(t, 1, countIn(t, env.client, table, "quarantined = TRUE"))

		rec.fail(nil)

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "b"}})
		}))

		relay.cycle(t.Context())

		test.SliceLen(t, 1, rec.payloads())
		test.EqOp(t, 1, countIn(t, env.client, table, "quarantined = TRUE"))
	})

	t.Run("holds a lease against a second claim", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)
		relay, _ := env.relay(t, c, table)

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}})
		}))

		claimed, err := relay.claim(t.Context())
		must.NoError(t, err)
		test.SliceLen(t, 1, claimed)

		again, err := relay.claim(t.Context())
		must.NoError(t, err)
		test.SliceEmpty(t, again)

		c.advance(DefaultLeaseDuration + time.Second)

		reclaimed, err := relay.claim(t.Context())
		must.NoError(t, err)
		test.SliceLen(t, 1, reclaimed)
	})

	t.Run("claims at most one message per partition key", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)
		relay, rec := env.relay(t, c, table)

		// This is the correlated NOT EXISTS subquery under a real planner.
		for _, id := range []string{"first", "second", "third"} {
			must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
				return w.Enqueue(t.Context(), q, Message{Topic: "orders", Key: "cart-1", Payload: map[string]any{"id": id}})
			}))
			c.advance(time.Millisecond)
		}

		for range 3 {
			relay.cycle(t.Context())
		}

		test.Eq(t, []string{`{"id":"first"}`, `{"id":"second"}`, `{"id":"third"}`}, rec.payloads())
	})

	t.Run("reaps published rows past retention", func(t *testing.T) {
		t.Parallel()

		c := newStubClock()
		table := env.newTable(t)
		w := env.writer(t, c, table)
		relay, _ := env.relay(t, c, table)

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "a"}})
		}))

		relay.cycle(t.Context())
		test.EqOp(t, 1, countIn(t, env.client, table, "published_at IS NOT NULL"))

		relay.reap(t.Context())
		test.EqOp(t, 1, countIn(t, env.client, table, "1=1"))

		c.advance(DefaultRetention + time.Hour)

		must.NoError(t, env.client.WithTransaction(t.Context(), func(q database.SQLQueryExecutor) error {
			return w.Enqueue(t.Context(), q, Message{Topic: "orders", Payload: map[string]any{"id": "b"}})
		}))

		// On MySQL this exercises the derived-table wrapper, without which the
		// server rejects reading the table being deleted from.
		relay.reap(t.Context())

		test.EqOp(t, 0, countIn(t, env.client, table, "published_at IS NOT NULL"))
		test.EqOp(t, 1, countIn(t, env.client, table, "published_at IS NULL"))
	})
}

func TestOutbox_Postgres(T *testing.T) {
	T.Parallel()

	pgtest.Run(T, func(ctx context.Context, pg *pgtest.Instance) {
		client, err := postgres.NewDatabaseClient(
			ctx,
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
			&testClientConfig{connectionString: pg.ConnectionString},
			nil,
		)
		must.NoError(T, err)
		T.Cleanup(func() { _ = client.Close() })

		// Both claim modes: SKIP LOCKED is the path that only a real server can
		// validate, and ClaimLease is what a single-relay deployment runs.
		for _, mode := range []ClaimMode{ClaimSkipLocked, ClaimLease} {
			T.Run(string(mode), func(t *testing.T) {
				t.Parallel()

				runDialectSuite(t, &dialectEnv{
					dialect:   DialectPostgres,
					claimMode: mode,
					client:    client,
				})
			})
		}
	}, pgtest.WithMaxOpenConns(32))
}

// runWithMySQL boots a MySQL container and hands its closure a database.Client
// against it. There is no mysqltest counterpart to pgtest, so the container
// setup mirrors the one in database/mysql/tableaccess.
func runWithMySQL(tb testing.TB, fn func(ctx context.Context, client database.Client)) {
	tb.Helper()

	containers.Run(tb,
		func(ctx context.Context) (*mysqlcontainers.MySQLContainer, error) {
			return mysqlcontainers.Run(
				ctx,
				defaultMySQLImage,
				mysqlcontainers.WithDatabase("outboxtest"),
				mysqlcontainers.WithUsername("outboxtest"),
				mysqlcontainers.WithPassword("outboxtest"),
				testcontainers.WithWaitStrategyAndDeadline(
					mysqlStartupDeadline,
					wait.ForLog("ready for connections").WithOccurrence(2),
				),
			)
		},
		func(ctx context.Context, container *mysqlcontainers.MySQLContainer) {
			// parseTime keeps DATETIME(6) round-tripping as time.Time rather
			// than []byte.
			connStr := container.MustConnectionString(ctx, "parseTime=true", "multiStatements=true")

			client, err := mysql.NewDatabaseClient(
				ctx,
				loggingnoop.NewLogger(),
				tracingnoop.NewTracerProvider(),
				&testClientConfig{connectionString: connStr},
				nil,
			)
			must.NoError(tb, err)
			tb.Cleanup(func() { _ = client.Close() })

			fn(ctx, client)
		},
	)
}

func TestOutbox_MySQL(T *testing.T) {
	T.Parallel()

	runWithMySQL(T, func(_ context.Context, client database.Client) {
		for _, mode := range []ClaimMode{ClaimSkipLocked, ClaimLease} {
			T.Run(string(mode), func(t *testing.T) {
				t.Parallel()

				runDialectSuite(t, &dialectEnv{
					dialect:   DialectMySQL,
					claimMode: mode,
					client:    client,
				})
			})
		}
	})
}

// TestMigrations_RealServers proves the shipped DDL is accepted verbatim by
// each server, independent of whether the relay then exercises every column.
func TestMigrations_RealServers(T *testing.T) {
	T.Parallel()

	T.Run("postgres", func(t *testing.T) {
		t.Parallel()

		pgtest.Run(t, func(ctx context.Context, pg *pgtest.Instance) {
			stmts, err := migrations.Statements(migrations.DialectPostgres, "ddl_check")
			must.NoError(t, err)

			for _, stmt := range stmts {
				_, execErr := pg.DB.ExecContext(ctx, stmt)
				must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
			}

			// Re-running must be a no-op: every statement is IF NOT EXISTS.
			for _, stmt := range stmts {
				_, execErr := pg.DB.ExecContext(ctx, stmt)
				must.NoError(t, execErr, must.Sprintf("re-executing %q", stmt))
			}
		})
	})

	T.Run("mysql", func(t *testing.T) {
		t.Parallel()

		runWithMySQL(t, func(ctx context.Context, client database.Client) {
			stmts, err := migrations.Statements(migrations.DialectMySQL, "ddl_check")
			must.NoError(t, err)

			// Executed twice: CREATE TABLE IF NOT EXISTS carries the inline KEY
			// clauses with it, so a second run must not trip over them.
			for range 2 {
				for _, stmt := range stmts {
					_, execErr := client.Writer().ExecContext(ctx, stmt)
					must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
				}
			}
		})
	})

	T.Run("statements carry no unrendered placeholder", func(t *testing.T) {
		t.Parallel()

		for _, d := range []migrations.Dialect{
			migrations.DialectPostgres, migrations.DialectMySQL, migrations.DialectSQLite,
		} {
			stmts, err := migrations.Statements(d, "ddl_check")
			must.NoError(t, err)

			for _, stmt := range stmts {
				test.False(t, strings.Contains(stmt, "{{"))
			}
		}
	})
}
