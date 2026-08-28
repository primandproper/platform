package querygen

import (
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// searchColumns is a directory table: an id, the scope it belongs to, the
// column a prefix is matched against, and the soft delete.
func searchColumns() []string {
	return []string{IDColumn, "scope", "username", CreatedAtColumn, ArchivedAtColumn}
}

func usernameSearch() PrefixSearch {
	return PrefixSearch{
		Column:    "username",
		Name:      "SearchUsersByUsername",
		CountName: "CountSearchUsersByUsername",
	}
}

func TestGenerator_PrefixSearchQueries(T *testing.T) {
	T.Parallel()

	T.Run("renders the set", func(t *testing.T) {
		t.Parallel()

		queries := pg().PrefixSearchQueries("users", searchColumns(), usernameSearch(), Match{Column: "scope"})

		must.SliceLen(t, 3, queries)

		// The page. Every argument reference is canonical — no bind markers —
		// because this is the text sqlc reads.
		test.EqOp(t, "SearchUsersByUsername", queries[0].Annotation.Name)
		test.EqOp(t, ManyType, queries[0].Annotation.Type)
		test.EqOp(t, `SELECT
	users.id,
	users.scope,
	users.username,
	users.created_at,
	users.archived_at
FROM users
WHERE users.archived_at IS NULL
	AND users.scope = sqlc.arg(scope)
	AND (users.username LIKE sqlc.arg(username_prefix)::text ESCAPE '!')
	AND users.username > COALESCE(sqlc.narg(page_cursor), '')
ORDER BY users.username ASC
LIMIT COALESCE(sqlc.narg(result_limit), 50);`, queries[0].Content)

		// The descending page: the same statement walking the searched column
		// the other way. A search takes a filter like any other paged read, and
		// the direction that filter carries is over this statement's own order.
		test.EqOp(t, "SearchUsersByUsernameDescending", queries[1].Annotation.Name)
		test.EqOp(t, ManyType, queries[1].Annotation.Type)
		test.EqOp(t, `SELECT
	users.id,
	users.scope,
	users.username,
	users.created_at,
	users.archived_at
FROM users
WHERE users.archived_at IS NULL
	AND users.scope = sqlc.arg(scope)
	AND (users.username LIKE sqlc.arg(username_prefix)::text ESCAPE '!')
	AND (users.username <= COALESCE(sqlc.narg(page_cursor), users.username) AND users.username <> COALESCE(sqlc.narg(page_cursor), ''))
ORDER BY users.username DESC
LIMIT COALESCE(sqlc.narg(result_limit), 50);`, queries[1].Content)

		// The count: the same predicates without the cursor, so the number
		// answers "how many rows match this prefix" rather than "how many are
		// left after where the caller has read to". One of it, because a count
		// does not depend on the order its rows would have come back in.
		test.EqOp(t, "CountSearchUsersByUsername", queries[2].Annotation.Name)
		test.EqOp(t, OneType, queries[2].Annotation.Type)
		test.EqOp(t, `SELECT COUNT(*)
FROM users
WHERE users.archived_at IS NULL
	AND users.scope = sqlc.arg(scope)
	AND (users.username LIKE sqlc.arg(username_prefix)::text ESCAPE '!');`, queries[2].Content)
	})

	T.Run("orders and pages by the searched column on every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			t.Run(string(d), func(t *testing.T) {
				t.Parallel()

				queries := For(d).PrefixSearchQueries("users", searchColumns(), usernameSearch(), Match{Column: "scope"})
				page := queries[0].Content

				// A cursor naming a position in an order the statement does not
				// use is a page that skips rows and repeats others, so the
				// three references to the column travel together.
				pattern := "sqlc.arg(username_prefix)"
				if d == dialect.Postgres {
					pattern += "::text"
				}

				test.StrContains(t, page, "(users.username LIKE "+pattern+" ESCAPE '!')")
				test.StrContains(t, page, "users.username > COALESCE(sqlc.narg(page_cursor), '')")
				test.StrContains(t, page, "ORDER BY users.username ASC")
				test.StrNotContains(t, page, "ORDER BY users.id")

				// And the descending half pages over the same column it orders
				// by, which is the property that makes it a keyset walk rather
				// than a reversed ordering with somebody else's cursor on it.
				descending := queries[1].Content
				test.StrContains(t, descending, "(users.username LIKE "+pattern+" ESCAPE '!')")
				test.StrContains(t, descending, "(users.username <= COALESCE(sqlc.narg(page_cursor), users.username) AND users.username <> COALESCE(sqlc.narg(page_cursor), ''))")
				test.StrContains(t, descending, "ORDER BY users.username DESC")
				test.StrNotContains(t, descending, "ORDER BY users.id")

				// Only the page size differs by dialect here — MySQL's LIMIT
				// takes a bare marker and nothing else.
				if d == dialect.MySQL {
					test.StrContains(t, page, "LIMIT ?")
				} else {
					test.StrContains(t, page, "LIMIT COALESCE(sqlc.narg(result_limit), 50)")
				}

				// The count is not a page: no cursor, no ordering, no limit.
				count := queries[2].Content
				test.StrNotContains(t, count, CursorArg)
				test.StrNotContains(t, count, "ORDER BY")
				test.StrNotContains(t, count, "LIMIT")
			})
		}
	})

	T.Run("renders no archived predicate for a table without the column", func(t *testing.T) {
		t.Parallel()

		queries := pg().PrefixSearchQueries("users", []string{IDColumn, "username"}, usernameSearch())

		for _, query := range queries {
			test.StrNotContains(t, query.Content, ArchivedAtColumn)
		}
	})

	T.Run("keys the page and the count on the same matches", func(t *testing.T) {
		t.Parallel()

		queries := pg().PrefixSearchQueries("users", searchColumns(), usernameSearch(),
			Match{Column: "scope"}, Match{Column: BelongsToAccountColumn})

		for _, query := range queries {
			test.StrContains(t, query.Content, "users.scope = sqlc.arg(scope)")
			test.StrContains(t, query.Content, "users.belongs_to_account = sqlc.arg(belongs_to_account)")
		}
	})

	T.Run("refuses a search column the table does not have", func(t *testing.T) {
		t.Parallel()

		err := recovered(func() {
			pg().PrefixSearchQueries("users", searchColumns(), PrefixSearch{
				Column:    "nickname",
				Name:      "SearchUsersByNickname",
				CountName: "CountSearchUsersByNickname",
			})
		})

		must.ErrorIs(t, err, ErrUnknownSearchColumn)
	})

	T.Run("refuses a search column that is not an identifier", func(t *testing.T) {
		t.Parallel()

		err := recovered(func() {
			pg().PrefixSearchQueries("users", searchColumns(), PrefixSearch{Column: "user name"})
		})

		must.ErrorIs(t, err, dialect.ErrInvalidIdentifier)
	})

	T.Run("refuses a pair that shares one name", func(t *testing.T) {
		t.Parallel()

		err := recovered(func() {
			pg().PrefixSearchQueries("users", searchColumns(), PrefixSearch{
				Column:    "username",
				Name:      "SearchUsersByUsername",
				CountName: "SearchUsersByUsername",
			})
		})

		must.ErrorIs(t, err, ErrDuplicateQueryName)
	})
}

// TestGenerator_PrefixSearchQueries_BindsEveryArgument pins the property the
// positional dialects notice and Postgres does not: a statement's placeholders
// and the arguments they stand for cannot disagree, whether the pattern is
// spliced once or the cursor twice.
//
// The descending page is the one that names an argument twice — it compares the
// column against the cursor and against the cursor's absence — so the two
// dialect readings of a repeat are both exercised here: Postgres numbers its
// markers and binds the value once, and the positional two take it again per
// occurrence.
func TestGenerator_PrefixSearchQueries_BindsEveryArgument(T *testing.T) {
	T.Parallel()

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			for _, query := range For(d).PrefixSearchQueries("users", searchColumns(), usernameSearch(), Match{Column: "scope"}) {
				sql, args := bindArguments(d, query.Content)

				test.StrNotContains(t, sql, "sqlc.")
				assertMarkersMatchArgs(t, d, Bound{SQL: sql, Args: args})
			}
		})
	}
}

func TestPrefixArg(t *testing.T) {
	t.Parallel()

	// Derived from the column so that a table searched on two of them names
	// them apart, and so a reader of the generated file can tell which column
	// an argument belongs to.
	test.EqOp(t, "username_prefix", PrefixArg("username"))
	test.EqOp(t, "email_address_prefix", PrefixArg("email_address"))
}

func TestPrefixPattern(t *testing.T) {
	t.Parallel()

	// Without the escaping a typed wildcard stays one, and the search widens
	// past the prefix somebody meant — which reads as a working search
	// returning too much rather than as a bug.
	test.EqOp(t, `ad%`, PrefixPattern("ad"))
	test.EqOp(t, `a!%%`, PrefixPattern("a%"))
	test.EqOp(t, `a!_%`, PrefixPattern("a_"))
	test.EqOp(t, `a!!%`, PrefixPattern("a!"))

	// A backslash is ordinary here, which is the point of not using it as the
	// escape character: there is no spelling of ESCAPE '\' that is right on
	// both MySQL and a MySQL with NO_BACKSLASH_ESCAPES set.
	test.EqOp(t, `a\%`, PrefixPattern(`a\`))

	// The escape rule must not double the escapes the wildcard rules introduce.
	test.EqOp(t, `!%!!%`, PrefixPattern("%!"))

	// An empty prefix is the whole table rather than nothing, which is what a
	// search box reads as before anybody types.
	test.EqOp(t, `%`, PrefixPattern(""))
}
