package migrate

import (
	"context"
	"database/sql"
	"embed"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"time"

	loggingnoop "github.com/primandproper/platform-go/v7/observability/logging/noop"
	"github.com/primandproper/platform-go/v7/testutils/containers"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	_ "modernc.org/sqlite"
)

//go:embed testdata/migrations/*.sql
var testMigrationsFS embed.FS

//go:embed testdata/unannotated/*.sql
var testUnannotatedFS embed.FS

const postgresImage = "postgres:17-alpine"

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

		_, err := New(DialectSQLite, nil)
		test.Error(t, err)
	})

	T.Run("rejects an unknown dialect", func(t *testing.T) {
		t.Parallel()

		_, err := New(Dialect("oracle"), testMigrations(t))
		test.Error(t, err)
	})

	T.Run("defaults the lock timeouts", func(t *testing.T) {
		t.Parallel()

		m, err := New(DialectSQLite, testMigrations(t))
		must.NoError(t, err)

		test.EqOp(t, DefaultLockProbeInterval, m.lockProbeInterval)
		test.EqOp(t, DefaultLockTimeout, m.lockTimeout)
		test.EqOp(t, DefaultUnlockProbeInterval, m.unlockProbeInterval)
		test.EqOp(t, DefaultUnlockTimeout, m.unlockTimeout)
	})

	T.Run("accepts overridden lock timeouts", func(t *testing.T) {
		t.Parallel()

		m, err := New(DialectSQLite, testMigrations(t),
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

				_, err := New(DialectSQLite, testMigrations(t), tc.opt)
				test.Error(t, err)
			})
		}
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

		m, err := New(DialectSQLite, testMigrations(t), WithLogger(loggingnoop.NewLogger()))
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

		m, err := New(DialectSQLite, testMigrations(t))
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

		m, err := New(DialectSQLite, sub, WithLogger(loggingnoop.NewLogger()))
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

	containers.SkipIfNotRunning(T)

	ctx := context.Background()
	container, err := containers.StartWithRetry(ctx, func(ctx context.Context) (*postgrescontainer.PostgresContainer, error) {
		return postgrescontainer.Run(
			ctx,
			postgresImage,
			postgrescontainer.WithDatabase("migratetest"),
			postgrescontainer.WithUsername("migratetest"),
			postgrescontainer.WithPassword("migratetest"),
			testcontainers.WithWaitStrategyAndDeadline(2*time.Minute, wait.ForLog("database system is ready to accept connections").WithOccurrence(2)),
		)
	})
	must.NoError(T, err)
	T.Cleanup(func() { _ = container.Terminate(context.Background()) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	must.NoError(T, err)

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

				m, newErr := New(DialectPostgres, testMigrations(t), WithLogger(loggingnoop.NewLogger()))
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
}
