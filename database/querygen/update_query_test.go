package querygen

import (
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
)

// updateQueryColumns is a conventional table, so the update stamps
// last_updated_at and the predicates exclude archived rows.
func updateQueryColumns() []string {
	return []string{IDColumn, "scope", "owner", "name", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}
}

// TestGenerator_UpdateQuery pins the property that is UpdateQuery's whole reason
// to exist, the one ListQuery has for the list: it is BoundUpdate's source text,
// named and annotated. Rewriting its argument references for a driver must yield
// exactly the SQL BoundUpdate executes — if the two ever render differently, a
// consumer's canonical .sql would carry a guarded write that sqlc checked while
// the store ran another one.
func TestGenerator_UpdateQuery(T *testing.T) {
	T.Parallel()

	columns := updateQueryColumns()
	setColumns := []string{"owner"}
	matches := []Match{
		{Column: "scope"},
		{Column: "owner", Arg: "current_owner"},
	}

	for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			q := For(d).UpdateQuery("TransferGadget", boundTable, columns, setColumns, nil, matches...)

			test.EqOp(t, "TransferGadget", q.Annotation.Name)

			// The count is the answer a guarded write gives: zero means the row
			// was not the one this caller thought it was.
			test.EqOp(t, ExecRowsType, q.Annotation.Type)

			// The canonical spelling: sqlc argument references, no bind markers.
			test.StrContains(t, q.Content, "sqlc.arg(owner)")
			test.StrContains(t, q.Content, "sqlc.arg(current_owner)")

			bound := For(d).BoundUpdate(boundTable, columns, setColumns, nil, matches...)

			sql, args := bindArguments(d, q.Content)
			test.EqOp(t, bound.SQL, sql)
			test.Eq(t, bound.Args, args)
		})
	}
}

// TestGenerator_UpdateQuery_AssignsOnlyWhatItIsHanded is the half of the shape
// that makes a field-specific write safe: the SET list is the columns named and
// nothing else, so a password change cannot blank a profile column that happened
// to be mutable.
func TestGenerator_UpdateQuery_AssignsOnlyWhatItIsHanded(t *testing.T) {
	t.Parallel()

	q := For(dialect.Postgres).UpdateQuery("SetGadgetName", boundTable, updateQueryColumns(),
		[]string{"name"}, nil, Match{Column: "scope"})

	test.StrContains(t, q.Content, "name = sqlc.arg(name)")
	test.StrNotContains(t, q.Content, "owner = sqlc.arg(owner)")

	// last_updated_at stamps by convention, from the server's clock, exactly as
	// the standard update does.
	test.StrContains(t, q.Content, LastUpdatedAtColumn+" = "+NowExpression)
	test.StrNotContains(t, q.Content, "sqlc.arg("+LastUpdatedAtColumn+")")
}
