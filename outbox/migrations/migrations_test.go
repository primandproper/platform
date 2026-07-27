package migrations

import (
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestStatements(T *testing.T) {
	T.Parallel()

	T.Run("renders the table name into every statement", func(t *testing.T) {
		t.Parallel()

		for _, d := range []Dialect{DialectPostgres, DialectMySQL, DialectSQLite} {
			stmts, err := Statements(d, "events_outbox")
			must.NoError(t, err)
			must.True(t, len(stmts) > 0)

			for _, stmt := range stmts {
				test.True(t, strings.Contains(stmt, "events_outbox"),
					test.Sprintf("dialect %q statement missing table name: %s", d, stmt))
				test.False(t, strings.Contains(stmt, tablePlaceholder),
					test.Sprintf("dialect %q left an unrendered placeholder", d))
			}
		}
	})

	T.Run("puts the table before its indexes", func(t *testing.T) {
		t.Parallel()

		// Postgres declares indexes separately, so ordering matters here in a
		// way it does not for MySQL's inline KEY clauses.
		stmts, err := Statements(DialectPostgres, "outbox_messages")
		must.NoError(t, err)
		must.True(t, len(stmts) >= 2)

		test.True(t, strings.HasPrefix(stmts[0], "CREATE TABLE"))
		for _, stmt := range stmts[1:] {
			test.True(t, strings.HasPrefix(stmt, "CREATE INDEX"))
		}
	})

	T.Run("strips comments and empty fragments", func(t *testing.T) {
		t.Parallel()

		for _, d := range []Dialect{DialectPostgres, DialectMySQL, DialectSQLite} {
			stmts, err := Statements(d, "outbox_messages")
			must.NoError(t, err)

			for _, stmt := range stmts {
				test.False(t, strings.Contains(stmt, "--"),
					test.Sprintf("dialect %q leaked a comment: %s", d, stmt))
				test.EqOp(t, stmt, strings.TrimSpace(stmt))
			}
		}
	})

	T.Run("survives a semicolon inside a comment", func(t *testing.T) {
		t.Parallel()

		// Regression: comments were stripped after splitting on ';', so a
		// comment containing a semicolon was torn in half and its tail arrived
		// at the head of the next statement as bogus SQL. MariaDB rejected the
		// result; Postgres never saw it, because only the MySQL DDL happened to
		// have prose with a semicolon in it.
		for _, d := range []Dialect{DialectPostgres, DialectMySQL, DialectSQLite} {
			stmts, err := Statements(d, "outbox_messages")
			must.NoError(t, err)

			for _, stmt := range stmts {
				test.True(t, strings.HasPrefix(stmt, "CREATE "),
					test.Sprintf("dialect %q produced a non-DDL fragment: %q", d, stmt))
			}
		}
	})

	T.Run("declares the columns the relay reads and writes", func(t *testing.T) {
		t.Parallel()

		required := []string{
			"id", "topic", "partition_key", "payload", "created_at",
			"next_attempt", "claimed_until", "published_at", "attempts", "last_error", "quarantined",
		}

		for _, d := range []Dialect{DialectPostgres, DialectMySQL, DialectSQLite} {
			stmts, err := Statements(d, "outbox_messages")
			must.NoError(t, err)

			for _, col := range required {
				test.True(t, strings.Contains(stmts[0], col),
					test.Sprintf("dialect %q is missing column %q", d, col))
			}
		}
	})

	T.Run("rejects an unsupported dialect", func(t *testing.T) {
		t.Parallel()

		_, err := Statements("cassandra", "outbox_messages")
		test.ErrorIs(t, err, ErrUnsupportedDialect)
	})

	T.Run("rejects a table name that is not an identifier", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"", "outbox messages", "outbox; DROP TABLE users", "1outbox"} {
			_, err := Statements(DialectPostgres, name)
			test.ErrorIs(t, err, ErrInvalidTableName,
				test.Sprintf("expected %q to be rejected", name))
		}
	})
}
