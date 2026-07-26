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
