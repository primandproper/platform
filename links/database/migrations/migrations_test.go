package migrations

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v14/database/ddl"
	"github.com/primandproper/platform-go/v14/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func allDialects() []dialect.Dialect {
	return []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite}
}

func TestStatements(T *testing.T) {
	T.Parallel()

	T.Run("renders every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			stmts, err := Statements(d, "")
			must.NoError(t, err)
			must.SliceNotEmpty(t, stmts, must.Sprintf("dialect %q", d))
		}
	})

	T.Run("rejects a dialect it has no schema for", func(t *testing.T) {
		t.Parallel()

		_, err := Statements(dialect.Dialect("oracle"), "")
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("substitutes the prefix everywhere", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			stmts, err := Statements(d, "custom")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			test.StrContains(t, joined, "custom_action_links", test.Sprintf("dialect %q", d))
			test.StrContains(t, joined, "custom_action_links_purge_after_idx", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, joined, ddl.Placeholder, test.Sprintf("dialect %q", d))
		}
	})

	// An empty namespace is the ordinary case, not a missing value: it renders
	// the component's own name, which is what a consumer with one application
	// per database wants.
	T.Run("an empty prefix renders the schema's own names", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			stmts, err := Statements(d, "")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			test.StrContains(t, joined, "action_links", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, joined, "_action_links", test.Sprintf("dialect %q", d))
		}
	})

	// An action link is minted, resolved once, and collected. archived_at would
	// keep rows nothing can read while making the sweep unable to reach the
	// rows it exists for, and last_updated_at would be a second copy of
	// resolved_at.
	T.Run("carries no convention triple", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			stmts, err := Statements(d, "")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			test.StrNotContains(t, joined, "archived_at", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, joined, "last_updated_at", test.Sprintf("dialect %q", d))
		}
	})

	// The two deadlines are separate columns because they answer different
	// questions: expires_at decides redemption, purge_after decides
	// collectability, and the gap between them is what lets a spent link be
	// told apart from one that never existed.
	T.Run("carries both deadlines", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			stmts, err := Statements(d, "")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			test.StrContains(t, joined, "expires_at", test.Sprintf("dialect %q", d))
			test.StrContains(t, joined, "purge_after", test.Sprintf("dialect %q", d))
		}
	})

	// resolved_at is what the resolution guards on, so it has to be the one
	// column a live row leaves NULL. A NOT NULL here would make single use
	// unexpressible as a statement.
	T.Run("leaves resolved_at nullable", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			stmts, err := Statements(d, "")
			must.NoError(t, err)

			joined := strings.Join(stmts, "\n")

			resolvedAt := strings.Index(joined, "resolved_at")
			must.True(t, resolvedAt >= 0, must.Sprintf("dialect %q", d))

			line := joined[resolvedAt : strings.Index(joined[resolvedAt:], "\n")+resolvedAt]
			test.StrNotContains(t, line, "NOT NULL", test.Sprintf("dialect %q", d))
		}
	})

	// An index cannot be created before the table it indexes.
	T.Run("creates the table before its index", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			stmts, err := Statements(d, "")
			must.NoError(t, err)
			must.SliceLen(t, 2, stmts, must.Sprintf("dialect %q", d))

			test.StrContains(t, stmts[0], "CREATE TABLE", test.Sprintf("dialect %q", d))
			test.StrContains(t, stmts[1], "INDEX", test.Sprintf("dialect %q", d))
		}
	})

	// MySQL has no CREATE INDEX IF NOT EXISTS, so its index is declared inside
	// the table. One statement is the right answer there, and two would be a
	// migration that fails on its second run.
	T.Run("mysql declares its index inline", func(t *testing.T) {
		t.Parallel()

		stmts, err := Statements(dialect.MySQL, "")
		must.NoError(t, err)
		must.SliceLen(t, 1, stmts)

		test.StrContains(t, stmts[0], "KEY action_links_purge_after_idx")
	})
}

func TestSQL(T *testing.T) {
	T.Parallel()

	T.Run("renders the same DDL as one body", func(t *testing.T) {
		t.Parallel()

		for _, d := range allDialects() {
			body, err := SQL(d, "ddb")
			must.NoError(t, err)

			stmts, stmtErr := Statements(d, "ddb")
			must.NoError(t, stmtErr)

			for _, stmt := range stmts {
				test.StrContains(t, body, strings.TrimSuffix(stmt, ";"), test.Sprintf("dialect %q", d))
			}
		}
	})

	T.Run("rejects a dialect it has no schema for", func(t *testing.T) {
		t.Parallel()

		_, err := SQL(dialect.Dialect("oracle"), "")
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})
}

func TestTables(T *testing.T) {
	T.Parallel()

	T.Run("names the table this package creates", func(t *testing.T) {
		t.Parallel()

		tables, err := Tables("")
		must.NoError(t, err)
		test.Eq(t, []string{"action_links"}, tables)

		tables, err = Tables("ddb")
		must.NoError(t, err)
		test.Eq(t, []string{"ddb_action_links"}, tables)
	})

	T.Run("rejects a prefix the schema cannot render", func(t *testing.T) {
		t.Parallel()

		tables, err := Tables("ddb_")
		test.Nil(t, tables)
		test.ErrorIs(t, err, ddl.ErrPrefixTrailingSeparator)
	})
}

func TestValidatePrefix(T *testing.T) {
	T.Parallel()

	T.Run("accepts a namespace the schema can render", func(t *testing.T) {
		t.Parallel()

		for _, prefix := range []string{"", "ddb", "app_two"} {
			test.NoError(t, ValidatePrefix(prefix), test.Sprintf("prefix %q", prefix))
		}
	})

	T.Run("rejects a namespace that is not an identifier fragment", func(t *testing.T) {
		t.Parallel()

		for _, prefix := range []string{"ddb-1", "a b", "links_; DROP TABLE users;--"} {
			test.Error(t, ValidatePrefix(prefix), test.Sprintf("prefix %q", prefix))
		}
	})

	// The index's name is the longest identifier this schema renders, so it is
	// the one a long prefix pushes over the limit. Catching it here turns a
	// migration that half ran into a config that would not load.
	T.Run("rejects a namespace that renders an over-long identifier", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, ValidatePrefix(strings.Repeat("a", ddl.MaxIdentifierLength)), ddl.ErrPrefixTooLong)
	})

	T.Run("rejects a namespace that ends in the separator", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, ValidatePrefix("ddb_"), ddl.ErrPrefixTrailingSeparator)
	})
}
