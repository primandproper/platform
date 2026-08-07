package dialect

import (
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestDialect_Valid(T *testing.T) {
	T.Parallel()

	T.Run("supported dialects", func(t *testing.T) {
		t.Parallel()

		for _, d := range []Dialect{Postgres, MySQL, SQLite} {
			test.True(t, d.Valid(), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("unknown and empty", func(t *testing.T) {
		t.Parallel()

		test.False(t, Dialect("oracle").Valid())
		test.False(t, Dialect("").Valid())
	})
}

func TestDialect_SupportsSkipLocked(T *testing.T) {
	T.Parallel()

	T.Run("per dialect", func(t *testing.T) {
		t.Parallel()

		test.True(t, Postgres.SupportsSkipLocked())
		test.True(t, MySQL.SupportsSkipLocked())
		test.False(t, SQLite.SupportsSkipLocked())
	})
}

func TestRequireDialect(T *testing.T) {
	T.Parallel()

	T.Run("accepts a dialect in the wanted set", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, RequireDialect("work queue", Postgres, Postgres))
		test.NoError(t, RequireDialect("outbox", MySQL, Postgres, MySQL))
	})

	T.Run("rejects a dialect outside the wanted set", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, RequireDialect("outbox", SQLite, Postgres, MySQL), ErrUnsupported)
	})

	// One dialect reads as itself; several read as a list.
	T.Run("renders the requirement", func(t *testing.T) {
		t.Parallel()

		one := RequireDialect("work queue", MySQL, Postgres)
		must.Error(t, one)
		test.StrContains(t, one.Error(), "requires postgres")

		several := RequireDialect("outbox", SQLite, Postgres, MySQL)
		must.Error(t, several)
		test.StrContains(t, several.Error(), "requires one of postgres, mysql")
	})

	// An empty set is a caller's bug, not a dialect that satisfies everything.
	T.Run("rejects an empty wanted set", func(t *testing.T) {
		t.Parallel()

		err := RequireDialect("work queue", Postgres)
		must.Error(t, err)

		test.ErrorIs(t, err, ErrUnsupported)
		test.StrContains(t, err.Error(), "no accepted dialects")
	})
}

func TestRequirePostgres(T *testing.T) {
	T.Parallel()

	T.Run("accepts Postgres", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, RequirePostgres("work queue", Postgres))
	})

	// Every caller wraps this, so errors.Is has to reach ErrUnsupported through
	// whatever context they add.
	T.Run("rejects every other dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range []Dialect{MySQL, SQLite, Dialect("nonsense"), ""} {
			test.ErrorIs(t, RequirePostgres("work queue", d), ErrUnsupported, test.Sprintf("dialect %q", d))
		}
	})

	// A process wiring several Postgres-only components needs to know which one
	// objected, so the component and the offending dialect are both in the text.
	T.Run("names the component and the dialect", func(t *testing.T) {
		t.Parallel()

		err := RequirePostgres("workqueue migration", MySQL)
		must.Error(t, err)

		test.StrContains(t, err.Error(), "workqueue migration")
		test.StrContains(t, err.Error(), "mysql")
	})
}

func TestDialect_Placeholder(T *testing.T) {
	T.Parallel()

	T.Run("postgres numbers its markers", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "$1", Postgres.Placeholder(1))
		test.EqOp(t, "$12", Postgres.Placeholder(12))
	})

	T.Run("mysql and sqlite do not", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "?", MySQL.Placeholder(3))
		test.EqOp(t, "?", SQLite.Placeholder(3))
	})
}

func TestDialect_Placeholders(T *testing.T) {
	T.Parallel()

	T.Run("postgres numbers from start", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "$3, $4, $5", Postgres.Placeholders(3, 3))
	})

	T.Run("mysql repeats", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "?, ?", MySQL.Placeholders(1, 2))
	})

	T.Run("zero count is empty", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", Postgres.Placeholders(1, 0))
	})
}

func TestValidIdentifier(T *testing.T) {
	T.Parallel()

	T.Run("accepts plain and schema-qualified names", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{"outbox_messages", "app.outbox_messages", "_t", "T1"} {
			test.True(t, ValidIdentifier(name), test.Sprintf("identifier %q", name))
		}
	})

	T.Run("rejects everything else", func(t *testing.T) {
		t.Parallel()

		for _, name := range []string{
			"", "1table", "a.b.c", "a-b", "a b", "a;drop table b", "a.", ".a",
			"outbox_messages\n", "naïve",
		} {
			test.False(t, ValidIdentifier(name), test.Sprintf("identifier %q", name))
		}
	})
}

func TestSplitStatements(T *testing.T) {
	T.Parallel()

	T.Run("splits on semicolons and trims", func(t *testing.T) {
		t.Parallel()

		stmts := SplitStatements("CREATE TABLE a (id int);\n\nCREATE INDEX b ON a (id);\n")

		test.Eq(t, []string{"CREATE TABLE a (id int)", "CREATE INDEX b ON a (id)"}, stmts)
	})

	T.Run("a comment containing a semicolon does not tear", func(t *testing.T) {
		t.Parallel()

		// The regression that motivated stripping before splitting: prose in a
		// comment carries a semicolon, and a naive split hands its tail to the
		// next statement as bogus SQL.
		ddl := "-- rows are marked, not deleted; the reaper removes them\nCREATE TABLE a (id int);"

		stmts := SplitStatements(ddl)

		test.SliceLen(t, 1, stmts)
		for _, stmt := range stmts {
			test.True(t, strings.HasPrefix(stmt, "CREATE"), test.Sprintf("statement %q", stmt))
		}
	})

	T.Run("comment-only input yields nothing", func(t *testing.T) {
		t.Parallel()

		test.SliceLen(t, 0, SplitStatements("-- nothing here\n\n-- or here\n"))
	})
}
