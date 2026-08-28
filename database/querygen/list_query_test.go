package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestGenerator_ListQueries pins the property that is ListQueries' whole reason
// to exist: it is BoundList's source text, named and annotated. Rewriting its
// argument references for a driver must yield exactly the SQL BoundList
// executes — if the two ever render differently, a keyed list variant in a
// consumer's canonical .sql would be checked while a different statement runs.
//
// Both directions, because both are executed. A corpus whose descending half
// was rendered by something other than the statement a store runs is the same
// gap the ascending half already closed, moved one statement over.
func TestGenerator_ListQueries(T *testing.T) {
	T.Parallel()

	columns := []string{IDColumn, "owner", "name", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}
	match := Match{Column: "owner"}

	directions := map[Direction]string{
		Ascending:  "ListWidgetsByOwner",
		Descending: "ListWidgetsByOwnerDescending",
	}

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			queries := For(d).ListQueries("ListWidgetsByOwner", "widgets", columns, match)
			must.SliceLen(t, 2, queries)

			for direction, name := range directions {
				q := pagedList(queries, direction)

				test.EqOp(t, name, q.Annotation.Name)
				test.EqOp(t, ManyType, q.Annotation.Type)

				// The canonical spelling: sqlc argument references, no bind markers.
				test.StrContains(t, q.Content, "sqlc.arg(owner)")

				bound := For(d).BoundList("widgets", columns, direction, match)

				sql, args := bindArguments(d, q.Content)
				test.EqOp(t, bound.SQL, sql)
				test.Eq(t, bound.Args, args)
			}
		})
	}
}

// TestGenerator_ListQueries_DifferByTheWalkAlone is what makes the pair a pair
// rather than two statements that happen to share a name: everything a filter
// decides — the window, the archived toggle, the keyed predicates, both counts
// — is the same text on both, and what differs is the cursor comparison and the
// ordering.
func TestGenerator_ListQueries_DifferByTheWalkAlone(T *testing.T) {
	T.Parallel()

	columns := []string{IDColumn, "owner", "name", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			g := For(d)
			queries := g.ListQueries("ListWidgetsByOwner", "widgets", columns, Match{Column: "owner"})

			ascending := pagedList(queries, Ascending).Content
			descending := pagedList(queries, Descending).Content

			// Substituting the two lines a direction is turns one into the
			// other exactly, so nothing else moved.
			rewritten := strings.Replace(ascending,
				"\tAND "+g.CursorCondition("widgets", Ascending)+"\n"+g.CursorLimitClause("widgets", Ascending),
				"\tAND "+g.CursorCondition("widgets", Descending)+"\n"+g.CursorLimitClause("widgets", Descending), 1)

			test.EqOp(t, descending, rewritten)
		})
	}
}
