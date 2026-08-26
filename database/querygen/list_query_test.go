package querygen

import (
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
)

// TestGenerator_ListQuery pins the property that is ListQuery's whole reason to
// exist: it is BoundList's source text, named and annotated. Rewriting its
// argument references for a driver must yield exactly the SQL BoundList
// executes — if the two ever render differently, a keyed list variant in a
// consumer's canonical .sql would be checked while a different statement runs.
func TestGenerator_ListQuery(T *testing.T) {
	T.Parallel()

	columns := []string{IDColumn, "owner", "name", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}
	match := Match{Column: "owner"}

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			q := For(d).ListQuery("ListWidgetsByOwner", "widgets", columns, match)

			test.EqOp(t, "ListWidgetsByOwner", q.Annotation.Name)
			test.EqOp(t, ManyType, q.Annotation.Type)

			// The canonical spelling: sqlc argument references, no bind markers.
			test.StrContains(t, q.Content, "sqlc.arg(owner)")

			bound := For(d).BoundList("widgets", columns, match)

			sql, args := bindArguments(d, q.Content)
			test.EqOp(t, bound.SQL, sql)
			test.Eq(t, bound.Args, args)
		})
	}
}
