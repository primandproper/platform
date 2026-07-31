package migrations

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v8/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestStatements(T *testing.T) {
	T.Parallel()

	T.Run("renders both tables for every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			t.Run(string(d), func(t *testing.T) {
				t.Parallel()

				stmts, err := Statements(d, "audit_")
				must.NoError(t, err)
				must.SliceNotEmpty(t, stmts)

				joined := strings.Join(stmts, "\n")
				test.StrContains(t, joined, "audit_entries")
				test.StrContains(t, joined, "audit_chains")
				test.StrNotContains(t, joined, prefixPlaceholder)

				// The uniqueness of (scope, seq) is the guarantee that a forked
				// chain cannot commit, so it is not optional in any dialect.
				test.StrContains(t, joined, "UNIQUE")

				for _, stmt := range stmts {
					test.StrNotContains(t, stmt, "--")
					test.StrNotContains(t, stmt, ";")
				}
			})
		}
	})

	T.Run("orders the table ahead of its indexes", func(t *testing.T) {
		t.Parallel()

		stmts, err := Statements(dialect.Postgres, "audit_")
		must.NoError(t, err)
		must.SliceNotEmpty(t, stmts)

		test.StrContains(t, stmts[0], "CREATE TABLE")
	})

	T.Run("accepts an empty prefix", func(t *testing.T) {
		t.Parallel()

		stmts, err := Statements(dialect.SQLite, "")
		must.NoError(t, err)
		test.StrContains(t, strings.Join(stmts, "\n"), "CREATE TABLE IF NOT EXISTS entries")
	})

	T.Run("rejects an unsupported dialect", func(t *testing.T) {
		t.Parallel()

		_, err := Statements("cassandra", "audit_")
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("rejects an unsafe prefix", func(t *testing.T) {
		t.Parallel()

		for _, prefix := range []string{"audit-", "audit_; DROP TABLE users; --", "1audit"} {
			_, err := Statements(dialect.Postgres, prefix)
			test.ErrorIs(t, err, ErrInvalidPrefix, test.Sprintf("prefix %q", prefix))
		}
	})
}

func TestSQL(T *testing.T) {
	T.Parallel()

	T.Run("joins the statements back into a migration body", func(t *testing.T) {
		t.Parallel()

		ddl, err := SQL(dialect.Postgres, "audit_")
		must.NoError(t, err)

		test.StrContains(t, ddl, "CREATE TABLE")
		test.StrHasSuffix(t, ";\n", ddl)

		// Comments are stripped before joining: goose splits on semicolons, and
		// a '--' comment containing one would be torn in half.
		test.StrNotContains(t, ddl, "--")
	})

	T.Run("propagates a rendering error", func(t *testing.T) {
		t.Parallel()

		_, err := SQL("cassandra", "audit_")
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})
}

func TestAppendOnlyStatements(T *testing.T) {
	T.Parallel()

	T.Run("renders an update-rejecting trigger for every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			t.Run(string(d), func(t *testing.T) {
				t.Parallel()

				stmts, err := AppendOnlyStatements(d, "audit_")
				must.NoError(t, err)
				must.SliceNotEmpty(t, stmts)

				joined := strings.Join(stmts, "\n")
				test.StrContains(t, joined, "audit_entries")
				test.StrContains(t, joined, "BEFORE UPDATE")
				test.StrContains(t, joined, appendOnlyMessage)

				// DELETE is deliberately not blocked: retention has to delete,
				// and the chain is what covers deletion instead.
				test.StrNotContains(t, joined, "BEFORE DELETE")
			})
		}
	})

	T.Run("keeps a plpgsql body whole", func(t *testing.T) {
		t.Parallel()

		stmts, err := AppendOnlyStatements(dialect.Postgres, "audit_")
		must.NoError(t, err)
		must.SliceLen(t, 2, stmts)

		// The function's body contains semicolons, which is exactly why these
		// are returned pre-split and never joined for a tool that would split
		// them again.
		test.StrContains(t, stmts[0], "RAISE EXCEPTION")
		test.StrContains(t, stmts[0], "LANGUAGE plpgsql")
		test.StrContains(t, stmts[1], "CREATE TRIGGER")
	})

	T.Run("rejects an unsupported dialect", func(t *testing.T) {
		t.Parallel()

		_, err := AppendOnlyStatements("cassandra", "audit_")
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("rejects an unsafe prefix", func(t *testing.T) {
		t.Parallel()

		_, err := AppendOnlyStatements(dialect.Postgres, "audit-")
		test.ErrorIs(t, err, ErrInvalidPrefix)
	})
}
