package pgtest

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewIsolationOptions(T *testing.T) {
	T.Parallel()

	T.Run("defaults to a deliberately small pool and no migration", func(t *testing.T) {
		t.Parallel()

		cfg := newIsolationOptions(nil)
		test.EqOp(t, DefaultIsolatedMaxOpenConns, cfg.maxOpenConns)
		test.EqOp(t, DefaultIsolatedMaxIdleConns, cfg.maxIdleConns)
		test.True(t, cfg.migrate == nil)
	})

	T.Run("options override defaults", func(t *testing.T) {
		t.Parallel()

		cfg := newIsolationOptions([]IsolationOption{
			nil,
			WithPoolSize(9, 3),
			WithMigration(func(context.Context, *sql.DB) error { return nil }),
		})
		test.EqOp(t, 9, cfg.maxOpenConns)
		test.EqOp(t, 3, cfg.maxIdleConns)
		test.True(t, cfg.migrate != nil)
	})
}

func TestSanitizeIdentifier(T *testing.T) {
	T.Parallel()

	T.Run("lowercases and replaces everything postgres would object to", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "testfoo_bar_baz_qux", sanitizeIdentifier("TestFoo/bar baz-qux", 64))
	})

	T.Run("trims the separators it would otherwise start or end with", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "abc", sanitizeIdentifier("//abc//", 64))
		test.EqOp(t, "", sanitizeIdentifier("////", 64))
	})

	T.Run("stops at the budget", func(t *testing.T) {
		t.Parallel()

		got := sanitizeIdentifier(strings.Repeat("a", 200), testNameBudget)
		test.EqOp(t, testNameBudget, len(got))
	})
}

func TestIsolationName(T *testing.T) {
	T.Parallel()

	T.Run("stays inside postgres' identifier limit", func(t *testing.T) {
		t.Parallel()

		got := isolationName(t, schemaPrefix)
		test.True(t, len(got) <= maxIdentifierLength)
		test.StrHasPrefix(t, schemaPrefix, got)
	})

	T.Run("two calls from one test do not collide", func(t *testing.T) {
		t.Parallel()

		// The random suffix is what makes the name unique: two long test names
		// truncate to the same prefix, and postgres would then silently land
		// them on one schema.
		test.NotEqOp(t, isolationName(t, schemaPrefix), isolationName(t, schemaPrefix))
	})

	T.Run("survives a test name longer than the whole identifier budget", func(t *testing.T) {
		t.Parallel()

		t.Run(strings.Repeat("wide", 40), func(t *testing.T) {
			t.Parallel()

			got := isolationName(t, clonePrefix)
			test.True(t, len(got) <= maxIdentifierLength)
			test.NotEqOp(t, got, isolationName(t, clonePrefix))
		})
	})
}

func TestQuoteIdentifier(T *testing.T) {
	T.Parallel()

	T.Run("quotes and escapes", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, `"plain"`, quoteIdentifier("plain"))
		test.EqOp(t, `"we""ird"`, quoteIdentifier(`we"ird`))
	})
}

func TestInstance_DSNRewriting(T *testing.T) {
	T.Parallel()

	const base = "postgres://platformtest:platformtest@127.0.0.1:54321/platformtest?sslmode=disable"

	T.Run("search_path joins the existing parameters", func(t *testing.T) {
		t.Parallel()

		instance := &Instance{ConnectionString: base}

		test.EqOp(t,
			"postgres://platformtest:platformtest@127.0.0.1:54321/platformtest?search_path=pgtest_x&sslmode=disable",
			instance.searchPathDSN(t, "pgtest_x"))
	})

	T.Run("database swap keeps everything else", func(t *testing.T) {
		t.Parallel()

		instance := &Instance{ConnectionString: base}

		test.EqOp(t,
			"postgres://platformtest:platformtest@127.0.0.1:54321/clone_x?sslmode=disable",
			instance.databaseDSN(t, "clone_x"))
	})
}

// createWidgets stands in for a real migration: unqualified DDL, exactly as
// goose would emit, landing in whatever schema or database it is handed.
func createWidgets(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE widgets (id BIGSERIAL PRIMARY KEY, label TEXT NOT NULL)`)

	return err
}

// countIn counts a table the connection resolves through its own search_path,
// which for a Schema pool is the table inside that schema and nothing else.
func countIn(tb testing.TB, ctx context.Context, db *sql.DB, table string) int {
	tb.Helper()

	var count int
	must.NoError(tb, db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quoteIdentifier(table))).Scan(&count))

	return count
}

func countSchemas(tb testing.TB, ctx context.Context, db *sql.DB, name string) int {
	tb.Helper()

	var count int
	must.NoError(tb, db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name = $1", name).Scan(&count))

	return count
}

func countDatabases(tb testing.TB, ctx context.Context, db *sql.DB, name string) int {
	tb.Helper()

	var count int
	must.NoError(tb, db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pg_database WHERE datname = $1", name).Scan(&count))

	return count
}

func TestInstance_Schema_Container(T *testing.T) {
	T.Parallel()

	Run(T, func(_ context.Context, pg *Instance) {
		T.Run("hands each test a private, migrated schema", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			first := pg.Schema(t, WithMigration(createWidgets))
			second := pg.Schema(t, WithMigration(createWidgets))

			test.NotEqOp(t, first.Name, second.Name)
			test.StrContains(t, first.ConnectionString, "search_path="+first.Name)

			// Same unqualified table name in both, and no interference.
			_, err := first.DB.ExecContext(ctx, `INSERT INTO widgets (label) VALUES ('a'), ('b')`)
			must.NoError(t, err)

			test.EqOp(t, 2, countIn(t, ctx, first.DB, "widgets"))
			test.EqOp(t, 0, countIn(t, ctx, second.DB, "widgets"))

			// And that really is the search_path doing it, not creation order.
			var current string
			must.NoError(t, first.DB.QueryRowContext(ctx, "SELECT current_schema()").Scan(&current))
			test.EqOp(t, first.Name, current)
		})

		T.Run("drops the schema when the test that took it ends", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			var name string

			t.Run("inner", func(t *testing.T) {
				name = pg.Schema(t, WithMigration(createWidgets)).Name

				test.EqOp(t, 1, countSchemas(t, t.Context(), pg.DB, name))
			})

			// The inner subtest's cleanups have run by the time t.Run returns.
			test.EqOp(t, 0, countSchemas(t, ctx, pg.DB, name))
		})

		T.Run("an unmigrated schema is empty rather than broken", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			isolated := pg.Schema(t)

			var tables int
			must.NoError(t, isolated.DB.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = $1", isolated.Name).Scan(&tables))
			test.EqOp(t, 0, tables)
		})
	})
}

func TestInstance_Template_Container(T *testing.T) {
	T.Parallel()

	Run(T, func(_ context.Context, pg *Instance) {
		template := pg.Template(T, WithMigration(func(ctx context.Context, db *sql.DB) error {
			if err := createWidgets(ctx, db); err != nil {
				return err
			}

			_, err := db.ExecContext(ctx, `INSERT INTO widgets (label) VALUES ('seeded')`)

			return err
		}))

		T.Run("clones carry the template's schema and data", func(t *testing.T) {
			t.Parallel()

			clone := template.Clone(t)

			test.NotEqOp(t, template.Name, clone.Name)
			test.StrContains(t, clone.ConnectionString, "/"+clone.Name)
			test.EqOp(t, 1, countIn(t, t.Context(), clone.DB, "widgets"))
		})

		T.Run("clones do not see each other's writes", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			first, second := template.Clone(t), template.Clone(t)

			_, err := first.DB.ExecContext(ctx, `INSERT INTO widgets (label) VALUES ('only-here')`)
			must.NoError(t, err)

			test.EqOp(t, 2, countIn(t, ctx, first.DB, "widgets"))
			test.EqOp(t, 1, countIn(t, ctx, second.DB, "widgets"))
		})

		T.Run("cloning still works while earlier clones are live", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			// The failure this guards is CREATE DATABASE ... TEMPLATE refusing to
			// run because a session is attached to the template — which is why
			// Template closes its migration pool before returning, and why a
			// clone's own sessions must not count against it.
			held := template.Clone(t)
			must.NoError(t, held.DB.PingContext(ctx))

			later := template.Clone(t)
			test.EqOp(t, 1, countIn(t, ctx, later.DB, "widgets"))
		})

		T.Run("drops the clone when the test that took it ends", func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			var name string

			t.Run("inner", func(t *testing.T) {
				name = template.Clone(t).Name

				test.EqOp(t, 1, countDatabases(t, t.Context(), pg.DB, name))
			})

			test.EqOp(t, 0, countDatabases(t, ctx, pg.DB, name))
		})
	})
}

// TestRun_DSNFromEnv_Container is not parallel, and neither is its subtest:
// t.Setenv refuses to run anywhere under a parallel test.
//
//nolint:paralleltest // t.Setenv forbids it, here and in every parent
func TestRun_DSNFromEnv_Container(T *testing.T) {
	Run(T, func(_ context.Context, pg *Instance) {
		T.Run("connects to the named server and starts no container", func(t *testing.T) { //nolint:paralleltest // t.Setenv forbids it
			const name = "PGTEST_LADDER_DSN"
			t.Setenv(name, pg.ConnectionString)

			var reached bool

			Run(t, func(ctx context.Context, external *Instance) {
				reached = true

				test.Nil(t, external.Container)
				test.EqOp(t, pg.ConnectionString, external.ConnectionString)
				test.EqOp(t, pg.Database, external.Database)
				test.EqOp(t, pg.Username, external.Username)
				test.EqOp(t, pg.Password, external.Password)
				test.EqOp(t, pg.Host, external.Host)
				test.EqOp(t, pg.Port, external.Port)

				var one int
				must.NoError(t, external.DB.QueryRowContext(ctx, "SELECT 1").Scan(&one))
				test.EqOp(t, 1, one)

				// The isolation helpers work off ConnectionString, so they work
				// against a server this process did not start either.
				isolated := external.Schema(t, WithMigration(createWidgets))
				test.EqOp(t, 0, countIn(t, ctx, isolated.DB, "widgets"))
			}, WithDSNFromEnv(name))

			test.True(t, reached)
		})
	})
}
