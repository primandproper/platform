package querygen

import (
	"strings"
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

	T.Run("renders the pair", func(t *testing.T) {
		t.Parallel()

		queries := pg().PrefixSearchQueries("users", searchColumns(), usernameSearch(), Match{Column: "scope"})

		must.SliceLen(t, 2, queries)

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

		// The count: the same predicates without the cursor, so the number
		// answers "how many rows match this prefix" rather than "how many are
		// left after where the caller has read to".
		test.EqOp(t, "CountSearchUsersByUsername", queries[1].Annotation.Name)
		test.EqOp(t, OneType, queries[1].Annotation.Type)
		test.EqOp(t, `SELECT COUNT(*)
FROM users
WHERE users.archived_at IS NULL
	AND users.scope = sqlc.arg(scope)
	AND (users.username LIKE sqlc.arg(username_prefix)::text ESCAPE '!');`, queries[1].Content)
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

				// Only the page size differs by dialect here — MySQL's LIMIT
				// takes a bare marker and nothing else.
				if d == dialect.MySQL {
					test.StrContains(t, page, "LIMIT ?")
				} else {
					test.StrContains(t, page, "LIMIT COALESCE(sqlc.narg(result_limit), 50)")
				}

				// The count is not a page: no cursor, no ordering, no limit.
				count := queries[1].Content
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

// TestGenerator_PrefixSearchQueries_BindsEveryArgumentOnce pins the property the
// positional dialects notice and Postgres does not: a statement's placeholders
// and the arguments they stand for cannot disagree, whether the pattern is
// spliced once or the scope three times.
func TestGenerator_PrefixSearchQueries_BindsEveryArgumentOnce(T *testing.T) {
	T.Parallel()

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			for _, query := range For(d).PrefixSearchQueries("users", searchColumns(), usernameSearch(), Match{Column: "scope"}) {
				sql, args := bindArguments(d, query.Content)

				// Neither statement binds a name twice — nothing here is
				// spliced the way the standard list splices its filter into two
				// count subqueries — so one marker per argument holds on the
				// numbered dialect as well as on the positional two.
				markers := strings.Count(sql, "?")
				if d == dialect.Postgres {
					markers = strings.Count(sql, "$")
				}

				test.StrNotContains(t, sql, "sqlc.")
				test.EqOp(t, markers, len(args), test.Sprintf("statement: %s", sql))
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
