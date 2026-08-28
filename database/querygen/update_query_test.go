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

// TestGenerator_UpdateQuery_GuardedWrite pins the shape that turns a
// field-specific write into a safe one: the guard names the value the row must
// still hold, under an argument of its own, so the SET list and the WHERE are
// two arguments rather than one. Under one name the statement would set the
// column to the value it was requiring it to already hold — legal SQL that two
// concurrent transfers both succeed at.
func TestGenerator_UpdateQuery_GuardedWrite(T *testing.T) {
	T.Parallel()

	columns := updateQueryColumns()
	setColumns := []string{"owner"}
	matches := []Match{
		{Column: "scope"},
		{Column: "owner", Arg: "current_owner"},
	}

	for _, d := range everyDialect() {
		T.Run(string(d), func(t *testing.T) {
			t.Parallel()

			q := For(d).UpdateQuery("TransferGadget", keyedTable, columns, setColumns, nil, matches...)

			test.EqOp(t, "TransferGadget", q.Annotation.Name)

			// The count is the answer a guarded write gives: zero means the row
			// was not the one this caller thought it was.
			test.EqOp(t, ExecRowsType, q.Annotation.Type)

			// The canonical spelling: sqlc argument references, no bind markers.
			test.StrContains(t, q.Content, "sqlc.arg(owner)")
			test.StrContains(t, q.Content, "sqlc.arg(current_owner)")

			// The two ends of the comparison are two arguments, in the order a
			// driver takes them: the assignment, the id, then the predicates.
			sql, args := bindQuery(d, q)

			test.Eq(t, []string{"owner", IDColumn, "scope", "current_owner"}, args)
			assertMarkersMatchArgs(t, d, sql, args)
		})
	}
}

// TestGenerator_UpdateQuery_AssignsOnlyWhatItIsHanded is the half of the shape
// that makes a field-specific write safe: the SET list is the columns named and
// nothing else, so a password change cannot blank a profile column that happened
// to be mutable.
func TestGenerator_UpdateQuery_AssignsOnlyWhatItIsHanded(t *testing.T) {
	t.Parallel()

	q := For(dialect.Postgres).UpdateQuery("SetGadgetName", keyedTable, updateQueryColumns(),
		[]string{"name"}, nil, Match{Column: "scope"})

	test.StrContains(t, q.Content, "name = sqlc.arg(name)")
	test.StrNotContains(t, q.Content, "owner = sqlc.arg(owner)")

	// last_updated_at stamps by convention, from the server's clock, exactly as
	// the standard update does.
	test.StrContains(t, q.Content, LastUpdatedAtColumn+" = "+NowExpression)
	test.StrNotContains(t, q.Content, "sqlc.arg("+LastUpdatedAtColumn+")")
}
