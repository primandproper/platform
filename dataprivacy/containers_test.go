package dataprivacy

import (
	"context"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	"github.com/primandproper/platform-go/v10/database/mysql"
	"github.com/primandproper/platform-go/v10/database/postgres"
	"github.com/primandproper/platform-go/v10/dataprivacy/migrations"
	"github.com/primandproper/platform-go/v10/testutils/containers/mysqltest"
	"github.com/primandproper/platform-go/v10/testutils/containers/pgtest"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// defaultMySQLImage pins the MariaDB flavor this suite exercises; mysqltest's
// default is stock MySQL.
const defaultMySQLImage = "mariadb:11"

// TestSQLStore_RealServers runs the same behavioral suite SQLite runs, against
// real servers.
//
// It exists because the SQL that only a real server can validate is otherwise
// merely rendered, never executed: numbered placeholders, SKIP LOCKED, MySQL's
// derived-table rewrite for the UPDATE and DELETE that read their own table,
// the partial indexes Postgres and SQLite have and MySQL does not, and native
// timestamp and boolean handling across three drivers.
//
// The lapse and reap queries are the ones this catches. Both wrap a subquery
// over the table they are modifying, which MySQL rejects outright without the
// derived table — and SQLite accepts either way, so it can never tell us.
func TestSQLStore_RealServers(T *testing.T) {
	T.Parallel()

	T.Run("postgres", func(t *testing.T) {
		t.Parallel()

		pgtest.Run(t, func(_ context.Context, pg *pgtest.Instance) {
			client, err := postgres.NewDatabaseClient(t.Context(),
				&testClientConfig{connectionString: pg.ConnectionString})
			must.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })

			runStoreSuite(t, &storeEnv{client: client, dialect: dialect.Postgres})
		})
	})

	T.Run("mysql", func(t *testing.T) {
		t.Parallel()

		runWithMySQL(t, func(_ context.Context, client database.Client) {
			runStoreSuite(t, &storeEnv{client: client, dialect: dialect.MySQL})
		})
	})
}

// TestMigrations_RealServers proves the shipped DDL is accepted verbatim by
// each server, independent of whether the store then exercises every column.
func TestMigrations_RealServers(T *testing.T) {
	T.Parallel()

	T.Run("postgres", func(t *testing.T) {
		t.Parallel()

		pgtest.Run(t, func(ctx context.Context, pg *pgtest.Instance) {
			stmts, err := migrations.Statements(dialect.Postgres, "ddl_check")
			must.NoError(t, err)

			// Executed twice: every statement is IF NOT EXISTS, so re-running a
			// migration must be a no-op rather than an error.
			for range 2 {
				for _, stmt := range stmts {
					_, execErr := pg.DB.ExecContext(ctx, stmt)
					must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
				}
			}
		})
	})

	T.Run("mysql", func(t *testing.T) {
		t.Parallel()

		runWithMySQL(t, func(ctx context.Context, client database.Client) {
			stmts, err := migrations.Statements(dialect.MySQL, "ddl_check")
			must.NoError(t, err)

			// MySQL has no CREATE INDEX IF NOT EXISTS, so unlike Postgres this
			// runs once — the table carries IF NOT EXISTS, the indexes cannot.
			for _, stmt := range stmts {
				_, execErr := client.Writer().ExecContext(ctx, stmt)
				must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
			}
		})
	})

	T.Run("statements carry no unrendered placeholder", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			stmts, err := migrations.Statements(d, "ddl_check")
			must.NoError(t, err)

			for _, stmt := range stmts {
				test.False(t, strings.Contains(stmt, "{{"))
			}
		}
	})
}

// runWithMySQL starts a MySQL-flavored container and hands the closure a
// database.Client against it.
func runWithMySQL(t *testing.T, fn func(ctx context.Context, client database.Client)) {
	t.Helper()

	mysqltest.Run(t, func(ctx context.Context, my *mysqltest.Instance) {
		client, err := mysql.NewDatabaseClient(ctx,
			&testClientConfig{connectionString: my.ConnectionString})
		must.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		fn(ctx, client)
	}, mysqltest.WithImage(defaultMySQLImage))
}
