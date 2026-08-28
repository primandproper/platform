package querygen

import (
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// roleColumns is the shape the delete exists for: a child table keyed on its
// parent and the value it grants, with no id and no convention triple.
var roleColumns = []string{"membership_id", "role"}

func TestDeleteQuery(T *testing.T) {
	T.Parallel()

	T.Run("annotates the statement as an execrows", func(t *testing.T) {
		t.Parallel()

		// The count is the answer: how many grants a clear destroyed, or
		// whether the row an erasure aimed at was there at all.
		query := For(dialect.Postgres).DeleteQuery("DeleteMembershipRoles", "membership_roles",
			roleColumns, Match{Column: "membership_id"})

		test.EqOp(t, "DeleteMembershipRoles", query.Annotation.Name)
		test.EqOp(t, ExecRowsType, query.Annotation.Type)
	})

	T.Run("keys an id-less table on its matches alone", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			query := For(d).DeleteQuery("DeleteMembershipRoles", "membership_roles",
				roleColumns, Match{Column: "membership_id"})

			test.EqOp(t, "DELETE FROM membership_roles\nWHERE membership_id = sqlc.arg(membership_id);", query.Content)
		}
	})

	T.Run("keys a conventional table on its id and its matches", func(t *testing.T) {
		t.Parallel()

		query := For(dialect.Postgres).DeleteQuery("EraseUser", "users",
			widgetsColumns(), Match{Column: BelongsToAccountColumn})

		test.StrContains(t, query.Content, "WHERE id = sqlc.arg(id)")
		test.StrContains(t, query.Content, "AND belongs_to_account = sqlc.arg(belongs_to_account)")
	})

	T.Run("renders no archived predicate", func(t *testing.T) {
		t.Parallel()

		// The one thing that separates this from every other statement built on
		// the same key. An erasure runs against a subject who was archived
		// first, so excluding archived rows would make it the write that cannot
		// reach the rows it exists for — and widgetsColumns has archived_at, so
		// the archive and the get both carry the predicate here.
		for _, d := range everyDialect() {
			query := For(d).DeleteQuery("EraseWidget", widgetsTable, widgetsColumns(),
				Match{Column: BelongsToAccountColumn})

			test.StrNotContains(t, query.Content, ArchivedAtColumn)
		}
	})

	T.Run("binds the key and nothing else", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			query := For(d).DeleteQuery("EraseWidget", widgetsTable, widgetsColumns(),
				Match{Column: BelongsToAccountColumn})

			_, args := bindArguments(d, query.Content)

			test.Eq(t, []string{IDColumn, BelongsToAccountColumn}, args)
		}
	})

	T.Run("qualifies nothing, as the other write verbs do not", func(t *testing.T) {
		t.Parallel()

		query := For(dialect.Postgres).DeleteQuery("EraseWidget", widgetsTable, widgetsColumns(),
			Match{Column: BelongsToAccountColumn})

		test.StrNotContains(t, query.Content, Qualify(widgetsTable, IDColumn))
	})

	T.Run("refuses a delete that keys on nothing", func(t *testing.T) {
		t.Parallel()

		// Without the refusal this is a truncate wearing a query name: an
		// id-less table with no match renders a DELETE with no WHERE at all,
		// and it would not even carry the archived predicate that makes the
		// unaddressable update merely wrong.
		err := recovered(func() {
			For(dialect.Postgres).DeleteQuery("DeleteMembershipRoles", "membership_roles", roleColumns)
		})

		must.Error(t, err)
		test.True(t, errors.Is(err, ErrUnaddressableRow))
		test.StrContains(t, err.Error(), "membership_roles")
	})

	T.Run("renders one text on all three dialects", func(t *testing.T) {
		t.Parallel()

		// Nothing in a keyed DELETE is a dialect's business — no expression, no
		// clause — so the three differ only where bindArguments spells the
		// markers.
		rendered := map[dialect.Dialect]string{}
		for _, d := range everyDialect() {
			rendered[d] = For(d).DeleteQuery("EraseWidget", widgetsTable, widgetsColumns(),
				Match{Column: BelongsToAccountColumn}).Content
		}

		test.EqOp(t, rendered[dialect.Postgres], rendered[dialect.MySQL])
		test.EqOp(t, rendered[dialect.Postgres], rendered[dialect.SQLite])
	})
}

func TestSingleRowPredicates_ArchivedStaysFirst(t *testing.T) {
	t.Parallel()

	// singleRowPredicates is keyPredicates with the archived predicate in front
	// of it, and every statement that carries both renders them in that order.
	// The split is what lets the delete take the key without the filter.
	predicates := For(dialect.Postgres).singleRowPredicates(widgetsTable, widgetsColumns(), BelongsToAccountColumn, true)

	must.SliceLen(t, 3, predicates)
	test.EqOp(t, Qualify(widgetsTable, ArchivedAtColumn)+" IS NULL", predicates[0])
	test.Eq(t, predicates[1:], For(dialect.Postgres).keyPredicates(widgetsTable, widgetsColumns(), BelongsToAccountColumn, true, nil))
}
