package migrate

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability/logging"
	loggingnoop "github.com/primandproper/platform-go/v8/observability/logging/noop"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	mockmetrics "github.com/primandproper/platform-go/v8/observability/metrics/mock"
	metricsnoop "github.com/primandproper/platform-go/v8/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v8/observability/tracing/noop"
	"github.com/primandproper/platform-go/v8/testutils/containers/pgtest"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	_ "modernc.org/sqlite"
)

// capturingLogger records the lines written through it. Every derivation
// (WithName, WithValues, ...) returns the same recorder, so lines a Migrator
// emits through its observer's derived logger are captured too; anything not
// overridden falls through to the no-op logger.
type capturingLogger struct {
	logging.Logger
	infos  []string
	errors []string
	mu     sync.Mutex
}

func newCapturingLogger() *capturingLogger {
	return &capturingLogger{Logger: loggingnoop.NewLogger()}
}

func (l *capturingLogger) Clone() logging.Logger                      { return l }
func (l *capturingLogger) WithName(string) logging.Logger             { return l }
func (l *capturingLogger) WithValues(map[string]any) logging.Logger   { return l }
func (l *capturingLogger) WithValue(string, any) logging.Logger       { return l }
func (l *capturingLogger) WithRequest(*http.Request) logging.Logger   { return l }
func (l *capturingLogger) WithResponse(*http.Response) logging.Logger { return l }
func (l *capturingLogger) WithError(error) logging.Logger             { return l }
func (l *capturingLogger) WithSpan(trace.Span) logging.Logger         { return l }

func (l *capturingLogger) Info(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos = append(l.infos, msg)
}

func (l *capturingLogger) Error(msg string, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, msg+": "+err.Error())
}

func (l *capturingLogger) snapshot() (infos, errs []string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.infos...), append([]string(nil), l.errors...)
}

//go:embed testdata/migrations/*.sql
var testMigrationsFS embed.FS

//go:embed testdata/unannotated/*.sql
var testUnannotatedFS embed.FS

func testMigrations(t *testing.T) fs.FS {
	t.Helper()

	sub, err := fs.Sub(testMigrationsFS, "testdata/migrations")
	must.NoError(t, err)

	return sub
}

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "migrate_test.db"))
	must.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var n int
	must.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&n))

	return n
}

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("rejects a nil filesystem", func(t *testing.T) {
		t.Parallel()

		_, err := New(dialect.SQLite, nil)
		test.Error(t, err)
	})

	T.Run("rejects an unknown dialect", func(t *testing.T) {
		t.Parallel()

		_, err := New(dialect.Dialect("oracle"), testMigrations(t))
		test.Error(t, err)
	})

	T.Run("defaults the lock timeouts", func(t *testing.T) {
		t.Parallel()

		m, err := New(dialect.SQLite, testMigrations(t))
		must.NoError(t, err)

		test.EqOp(t, DefaultLockProbeInterval, m.lockProbeInterval)
		test.EqOp(t, DefaultLockTimeout, m.lockTimeout)
		test.EqOp(t, DefaultUnlockProbeInterval, m.unlockProbeInterval)
		test.EqOp(t, DefaultUnlockTimeout, m.unlockTimeout)
	})

	T.Run("accepts overridden lock timeouts", func(t *testing.T) {
		t.Parallel()

		m, err := New(dialect.SQLite, testMigrations(t),
			WithLockTimeout(5*time.Second, 10*time.Minute),
			WithUnlockTimeout(2*time.Second, time.Minute),
		)
		must.NoError(t, err)

		test.EqOp(t, 5*time.Second, m.lockProbeInterval)
		test.EqOp(t, 10*time.Minute, m.lockTimeout)
	})

	T.Run("rejects timeouts goose cannot express", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			opt  Option
			name string
		}{
			{name: "sub-second lock probe", opt: WithLockTimeout(500*time.Millisecond, time.Minute)},
			{name: "fractional lock probe", opt: WithLockTimeout(1500*time.Millisecond, time.Minute)},
			{name: "timeout below one probe", opt: WithLockTimeout(10*time.Second, 5*time.Second)},
			{name: "zero lock probe", opt: WithLockTimeout(0, time.Minute)},
			{name: "sub-second unlock probe", opt: WithUnlockTimeout(500*time.Millisecond, time.Minute)},
			{name: "unlock timeout below one probe", opt: WithUnlockTimeout(10*time.Second, time.Second)},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				_, err := New(dialect.SQLite, testMigrations(t), tc.opt)
				test.Error(t, err)
			})
		}
	})
}

func TestNew_Options(T *testing.T) {
	T.Parallel()

	T.Run("nil options are skipped and the rest are applied", func(t *testing.T) {
		t.Parallel()

		m, err := New(dialect.Postgres, testMigrations(t),
			nil,
			WithLogger(loggingnoop.NewLogger()),
			WithTracerProvider(tracingnoop.NewTracerProvider()),
			WithMetricsProvider(metricsnoop.NewMetricsProvider()),
			WithLockKey("tenant-a"),
			WithoutLock(),
		)
		must.NoError(t, err)

		test.EqOp(t, "tenant-a", m.lockKey)
		test.True(t, m.withoutLock)
	})

	T.Run("an instrument that cannot be built fails construction", func(t *testing.T) {
		t.Parallel()

		counters := []string{
			"database_migrator_runs",
			"database_migrator_applied",
			"database_migrator_errors",
		}

		for i, failing := range counters {
			t.Run(failing, func(t *testing.T) {
				t.Parallel()

				mp := &mockmetrics.ProviderMock{
					NewInt64CounterFunc: func(name string, _ ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
						if name == failing {
							return nil, platformerrors.New("instrument unavailable")
						}

						return &mockmetrics.Int64CounterMock{}, nil
					},
				}

				_, err := New(dialect.SQLite, testMigrations(t), WithMetricsProvider(mp))
				must.Error(t, err)
				test.SliceLen(t, i+1, mp.NewInt64CounterCalls())
			})
		}

		t.Run("database_migrator_latency_ms", func(t *testing.T) {
			t.Parallel()

			mp := &mockmetrics.ProviderMock{
				NewInt64CounterFunc: func(string, ...metric.Int64CounterOption) (metrics.Int64Counter, error) {
					return &mockmetrics.Int64CounterMock{}, nil
				},
				NewFloat64HistogramFunc: func(string, ...metric.Float64HistogramOption) (metrics.Float64Histogram, error) {
					return nil, platformerrors.New("instrument unavailable")
				},
			}

			_, err := New(dialect.SQLite, testMigrations(t), WithMetricsProvider(mp))
			must.Error(t, err)
			test.SliceLen(t, 1, mp.NewFloat64HistogramCalls())
		})
	})
}

func TestGooseDialect(T *testing.T) {
	T.Parallel()

	T.Run("maps every supported dialect", func(t *testing.T) {
		t.Parallel()

		for _, tc := range []struct {
			in   dialect.Dialect
			want goose.Dialect
		}{
			{in: dialect.Postgres, want: goose.DialectPostgres},
			{in: dialect.MySQL, want: goose.DialectMySQL},
			{in: dialect.SQLite, want: goose.DialectSQLite3},
		} {
			got, err := gooseDialect(tc.in)
			must.NoError(t, err)
			test.EqOp(t, tc.want, got)
		}
	})

	T.Run("rejects an unknown dialect", func(t *testing.T) {
		t.Parallel()

		_, err := gooseDialect(dialect.Dialect("oracle"))
		test.Error(t, err)
	})
}

func TestGooseLogger(T *testing.T) {
	T.Parallel()

	T.Run("routes goose output through the platform logger", func(t *testing.T) {
		t.Parallel()

		cl := newCapturingLogger()
		g := &gooseLogger{logger: cl}

		g.Printf("applied %d migrations", 3)
		// Fatalf must not exit: goose calls it for conditions it also returns
		// as errors, and the test reaching its assertions is the proof.
		g.Fatalf("migration %s failed", "00003_orders.sql")

		infos, errs := cl.snapshot()
		must.SliceLen(t, 1, infos)
		test.EqOp(t, "applied 3 migrations", infos[0])
		must.SliceLen(t, 1, errs)
		test.StrContains(t, errs[0], "00003_orders.sql failed")
	})
}

func TestGooseProbe(T *testing.T) {
	T.Parallel()

	T.Run("splits a timeout into period and threshold", func(t *testing.T) {
		t.Parallel()

		// The defaults must survive the round trip as goose's original
		// hardcoded (1, 60) and (1, 30).
		period, threshold, err := gooseProbe("lock", DefaultLockProbeInterval, DefaultLockTimeout)
		must.NoError(t, err)
		test.EqOp(t, uint64(1), period)
		test.EqOp(t, uint64(60), threshold)

		period, threshold, err = gooseProbe("unlock", DefaultUnlockProbeInterval, DefaultUnlockTimeout)
		must.NoError(t, err)
		test.EqOp(t, uint64(1), period)
		test.EqOp(t, uint64(30), threshold)
	})

	T.Run("period times threshold is the requested timeout", func(t *testing.T) {
		t.Parallel()

		period, threshold, err := gooseProbe("lock", 5*time.Second, 10*time.Minute)
		must.NoError(t, err)
		test.EqOp(t, uint64(5), period)
		test.EqOp(t, uint64(120), threshold)
		test.EqOp(t, 10*time.Minute, time.Duration(period*threshold)*time.Second)
	})

	T.Run("a timeout of exactly one probe is allowed", func(t *testing.T) {
		t.Parallel()

		_, threshold, err := gooseProbe("lock", time.Second, time.Second)
		must.NoError(t, err)
		test.EqOp(t, uint64(1), threshold)
	})
}

func TestMigrator_Migrate_SQLite(T *testing.T) {
	T.Parallel()

	T.Run("applies all migrations and is idempotent", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		db := openSQLite(t)

		m, err := New(dialect.SQLite, testMigrations(t), WithLogger(loggingnoop.NewLogger()))
		must.NoError(t, err)

		must.NoError(t, m.Migrate(ctx, db))

		// All migrations landed: the tables exist and are queryable — including
		// both tables created by the single multi-statement migration.
		test.EqOp(t, 0, countRows(t, db, "migrate_test_users"))
		test.EqOp(t, 0, countRows(t, db, "migrate_test_widgets"))
		test.EqOp(t, 0, countRows(t, db, "migrate_test_orders"))
		test.EqOp(t, 0, countRows(t, db, "migrate_test_order_items"))

		// A second run is a no-op, not an error.
		must.NoError(t, m.Migrate(ctx, db))
	})

	T.Run("rejects a nil database", func(t *testing.T) {
		t.Parallel()

		m, err := New(dialect.SQLite, testMigrations(t))
		must.NoError(t, err)

		test.Error(t, m.Migrate(t.Context(), nil))
	})

	T.Run("applies migrations that carry no goose annotations", func(t *testing.T) {
		t.Parallel()

		// The end-to-end claim: plain SQL in a numbered file is a valid
		// migration, and the multi-statement one proves the splitter still
		// runs every statement rather than only the first.
		ctx := t.Context()
		db := openSQLite(t)

		sub, err := fs.Sub(testUnannotatedFS, "testdata/unannotated")
		must.NoError(t, err)

		m, err := New(dialect.SQLite, sub, WithLogger(loggingnoop.NewLogger()))
		must.NoError(t, err)

		must.NoError(t, m.Migrate(ctx, db))

		test.EqOp(t, 0, countRows(t, db, "migrate_bare_users"))
		test.EqOp(t, 0, countRows(t, db, "migrate_bare_widgets"))

		// The index is the trailing statement of the multi-statement file, so
		// it is the part that goes missing if only the first statement of a
		// section runs. The table existing does not prove that.
		var indexes int
		must.NoError(t, db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'migrate_bare_widgets_by_label'`,
		).Scan(&indexes))
		test.EqOp(t, 1, indexes)

		// Idempotent, same as the annotated path.
		must.NoError(t, m.Migrate(ctx, db))
	})
}

func TestMigrator_Migrate_Failures(T *testing.T) {
	T.Parallel()

	T.Run("an unresolvable dialect is reported", func(t *testing.T) {
		t.Parallel()

		m, err := New(dialect.SQLite, testMigrations(t), WithLogger(loggingnoop.NewLogger()))
		must.NoError(t, err)

		// New rejects an unknown dialect, so the only way to reach Migrate's
		// own guard is to corrupt the field behind its back.
		m.dialect = dialect.Dialect("oracle")

		test.Error(t, m.Migrate(t.Context(), openSQLite(t)))
	})

	T.Run("a database that cannot be reached is reported", func(t *testing.T) {
		t.Parallel()

		db := openSQLite(t)
		must.NoError(t, db.Close())

		m, err := New(dialect.SQLite, testMigrations(t), WithLogger(loggingnoop.NewLogger()))
		must.NoError(t, err)

		test.Error(t, m.Migrate(t.Context(), db))
	})

	T.Run("the locked path is taken for postgres", func(t *testing.T) {
		t.Parallel()

		cl := newCapturingLogger()

		// A postgres Migrator over a SQLite handle: enough to build the session
		// locker and log the wait, and guaranteed to fail once goose actually
		// speaks postgres to it. That failure is the point — it proves the
		// locked branch ran without needing a container.
		m, err := New(dialect.Postgres, testMigrations(t), WithLockKey("scoped"), WithLogger(cl))
		must.NoError(t, err)

		test.Error(t, m.Migrate(t.Context(), openSQLite(t)))

		infos, _ := cl.snapshot()
		test.SliceContains(t, infos, "acquiring migration lock and applying migrations")
	})

	T.Run("WithoutLock skips the session locker", func(t *testing.T) {
		t.Parallel()

		cl := newCapturingLogger()

		m, err := New(dialect.Postgres, testMigrations(t), WithoutLock(), WithLogger(cl))
		must.NoError(t, err)

		test.Error(t, m.Migrate(t.Context(), openSQLite(t)))

		infos, _ := cl.snapshot()
		test.SliceNotContains(t, infos, "acquiring migration lock and applying migrations")
	})
}

func TestLockID(T *testing.T) {
	T.Parallel()

	T.Run("stable per key, distinct across keys", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, lockID("a"), lockID("a"))
		test.NotEqOp(t, lockID("a"), lockID("b"))
		test.NotEqOp(t, lockID(""), lockID("a"))
	})
}

func TestMigrator_Migrate_PostgresContainer(T *testing.T) {
	T.Parallel()

	// The subtest opens its own pools off connStr, one per simulated replica.
	pgtest.Run(T, func(_ context.Context, pg *pgtest.Instance) {
		connStr := pg.ConnectionString

		T.Run("concurrent replicas serialize on the session lock", func(t *testing.T) {
			t.Parallel()

			const replicas = 3

			// Each "replica" gets its own *sql.DB, as separate processes would.
			errs := make([]error, replicas)
			var wg sync.WaitGroup
			for idx := range replicas {
				wg.Go(func() {
					db, openErr := sql.Open("pgx", connStr)
					if openErr != nil {
						errs[idx] = openErr
						return
					}
					defer func() { _ = db.Close() }()

					m, newErr := New(dialect.Postgres, testMigrations(t), WithLogger(loggingnoop.NewLogger()))
					if newErr != nil {
						errs[idx] = newErr
						return
					}

					errs[idx] = m.Migrate(t.Context(), db)
				})
			}
			wg.Wait()

			for idx, migrateErr := range errs {
				if migrateErr != nil {
					t.Fatalf("replica %d failed: %v", idx, migrateErr)
				}
			}

			// The winner migrated, the others waited and no-opped; the schema is
			// whole, including both tables from the multi-statement migration.
			db, openErr := sql.Open("pgx", connStr)
			must.NoError(t, openErr)
			defer func() { _ = db.Close() }()

			test.EqOp(t, 0, countRows(t, db, "migrate_test_users"))
			test.EqOp(t, 0, countRows(t, db, "migrate_test_widgets"))
			test.EqOp(t, 0, countRows(t, db, "migrate_test_orders"))
			test.EqOp(t, 0, countRows(t, db, "migrate_test_order_items"))
		})
	}, pgtest.WithCredentials("migratetest", "migratetest", "migratetest"))
}
