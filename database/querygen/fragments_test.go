package querygen

import (
	"strings"
	"testing"

	"github.com/shoenig/test"
)

func TestJoinStatement_String(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		got := JoinStatement{
			JoinTarget:   "things",
			TargetColumn: "stuff_id",
			OnTable:      "stuff",
			OnColumn:     IDColumn,
		}.String()

		test.EqOp(t, "JOIN things ON stuff.id=things.stuff_id", got)
	})
}

func TestILIKECondition(T *testing.T) {
	T.Parallel()

	T.Run("binds the term and supplies the wildcards itself", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, `ILIKE '%' || sqlc.arg(name_query)::text || '%'`, ILIKECondition("name_query"))
	})
}

func TestCursorCondition(T *testing.T) {
	T.Parallel()

	T.Run("an absent cursor coalesces rather than branching", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "things.id > COALESCE(sqlc.narg(cursor), '')", CursorCondition("things"))
	})
}

func TestCursorLimitClause(T *testing.T) {
	T.Parallel()

	T.Run("orders by the column the cursor names", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "ORDER BY things.id ASC\nLIMIT COALESCE(sqlc.narg(result_limit), 50)", CursorLimitClause("things"))
	})
}

func TestCursorPaginationFragment(T *testing.T) {
	T.Parallel()

	T.Run("prefixes the predicate with AND, for the tail of a WHERE", func(t *testing.T) {
		t.Parallel()

		want := "AND things.id > COALESCE(sqlc.narg(cursor), '')\n" +
			"ORDER BY things.id ASC\nLIMIT COALESCE(sqlc.narg(result_limit), 50)"

		test.EqOp(t, want, CursorPaginationFragment("things"))
	})
}

func TestReindexScanQuery(T *testing.T) {
	T.Parallel()

	T.Run("walks ids in byte order", func(t *testing.T) {
		t.Parallel()

		want := `SELECT things.id
FROM things
WHERE things.archived_at IS NULL
	AND things.id COLLATE "C" > sqlc.arg(cursor)
ORDER BY things.id COLLATE "C"
LIMIT COALESCE(sqlc.narg(result_limit), 50);`

		test.EqOp(t, want, ReindexScanQuery("things"))
	})

	T.Run("both the comparison and the ordering carry the collation", func(t *testing.T) {
		t.Parallel()

		// Either one alone is the failure mode search/sync's pruner cannot
		// survive: a walk ordered one way and resumed the other skips rows, and
		// the pruner reads a skipped row as a document to delete.
		test.EqOp(t, 2, strings.Count(ReindexScanQuery("things"), `COLLATE "C"`))
	})
}

func TestFilterConditions(T *testing.T) {
	T.Parallel()

	T.Run("renders the whole window for a conventional table", func(t *testing.T) {
		t.Parallel()

		want := `things.created_at > COALESCE(sqlc.narg(created_after), (SELECT NOW() - '999 years'::INTERVAL))
	AND things.created_at < COALESCE(sqlc.narg(created_before), (SELECT NOW() + '999 years'::INTERVAL))
	AND (
		things.last_updated_at IS NULL
		OR things.last_updated_at > COALESCE(sqlc.narg(updated_after), (SELECT NOW() - '999 years'::INTERVAL))
	)
	AND (
		things.last_updated_at IS NULL
		OR things.last_updated_at < COALESCE(sqlc.narg(updated_before), (SELECT NOW() + '999 years'::INTERVAL))
	)
	AND (COALESCE(sqlc.narg(include_archived), false)::boolean OR things.archived_at IS NULL)
	AND things.id > COALESCE(sqlc.narg(cursor), '')`

		test.EqOp(t, want, FilterConditions("things", columnsFor()))
	})

	T.Run("is the whole clause, so include_archived is the only thing deciding", func(t *testing.T) {
		t.Parallel()

		// A bare archived_at IS NULL anywhere in the clause would make the flag
		// decorative: the rows it admits would already have been excluded.
		got := FilterConditions("things", columnsFor())

		test.EqOp(t, 1, strings.Count(got, ArchivedAtColumn+" IS NULL"))
		test.StrContains(t, got, "sqlc.narg("+IncludeArchivedArg+")")
	})

	T.Run("omits the predicates whose columns the table lacks", func(t *testing.T) {
		t.Parallel()

		got := FilterConditions("things", []string{IDColumn, "name"})

		test.EqOp(t, "things.id > COALESCE(sqlc.narg(cursor), '')", got)
	})

	T.Run("places the caller's conditions before the cursor", func(t *testing.T) {
		t.Parallel()

		got := FilterConditions("things", []string{IDColumn}, "things.kind = sqlc.arg(kind)")

		test.EqOp(t, "things.kind = sqlc.arg(kind)\n\tAND things.id > COALESCE(sqlc.narg(cursor), '')", got)
	})
}

func TestFilterCountSelect(T *testing.T) {
	T.Parallel()

	T.Run("counts the filter window without the cursor", func(t *testing.T) {
		t.Parallel()

		got := FilterCountSelect("things", columnsFor(), nil)

		// A count that moved with the cursor would report the rows remaining
		// rather than the rows matching, so it would shrink to zero as the
		// caller pages through the very rows it is supposed to be counting.
		test.StrNotContains(t, got, CursorArg)
		test.StrContains(t, got, "sqlc.narg("+CreatedAfterArg+")")
		test.True(t, strings.HasSuffix(got, ") AS filtered_count"))
	})

	T.Run("indents to sit inside a select list", func(t *testing.T) {
		t.Parallel()

		want := `(
		SELECT COUNT(things.id)
		FROM things
		WHERE things.created_at > COALESCE(sqlc.narg(created_after), (SELECT NOW() - '999 years'::INTERVAL))
			AND things.created_at < COALESCE(sqlc.narg(created_before), (SELECT NOW() + '999 years'::INTERVAL))
	) AS filtered_count`

		test.EqOp(t, want, FilterCountSelect("things", []string{IDColumn, CreatedAtColumn}, nil))
	})

	T.Run("renders the joins it is given", func(t *testing.T) {
		t.Parallel()

		join := JoinStatement{JoinTarget: "stuff", TargetColumn: IDColumn, OnTable: "things", OnColumn: "stuff_id"}

		got := FilterCountSelect("things", []string{IDColumn}, []string{join.String()})

		test.StrContains(t, got, "\n\t\tJOIN stuff ON things.stuff_id=stuff.id")
	})

	T.Run("omits a WHERE it would have nothing to put after", func(t *testing.T) {
		t.Parallel()

		got := FilterCountSelect("things", []string{IDColumn, "name"}, nil)

		test.EqOp(t, "(\n\t\tSELECT COUNT(things.id)\n\t\tFROM things\n\t) AS filtered_count", got)
	})
}

func TestTotalCountSelect(T *testing.T) {
	T.Parallel()

	T.Run("applies the same archived toggle as the filter", func(t *testing.T) {
		t.Parallel()

		want := `(
		SELECT COUNT(things.id)
		FROM things
		WHERE (COALESCE(sqlc.narg(include_archived), false)::boolean OR things.archived_at IS NULL)
	) AS total_count`

		test.EqOp(t, want, TotalCountSelect("things", columnsFor(), nil))
	})

	T.Run("ignores the time window, which is what makes it the total", func(t *testing.T) {
		t.Parallel()

		got := TotalCountSelect("things", columnsFor(), nil)

		test.StrNotContains(t, got, CreatedAfterArg)
		test.StrNotContains(t, got, UpdatedAfterArg)
		test.StrNotContains(t, got, CursorArg)
	})

	T.Run("keeps the caller's scoping conditions, which are not part of the window", func(t *testing.T) {
		t.Parallel()

		got := TotalCountSelect("things", columnsFor(), nil, "things.belongs_to_account = sqlc.arg(belongs_to_account)")

		test.StrContains(t, got, "AND things.belongs_to_account = sqlc.arg(belongs_to_account)")
	})

	T.Run("omits a WHERE it would have nothing to put after", func(t *testing.T) {
		t.Parallel()

		got := TotalCountSelect("things", []string{IDColumn}, nil)

		test.EqOp(t, "(\n\t\tSELECT COUNT(things.id)\n\t\tFROM things\n\t) AS total_count", got)
	})
}

func TestJoinPredicates(T *testing.T) {
	T.Parallel()

	T.Run("re-indents a multi-line predicate to its new depth", func(t *testing.T) {
		t.Parallel()

		got := joinPredicates([]string{"a = 1", "(\n\tb IS NULL\n\tOR b = 2\n)"}, "\t\t")

		test.EqOp(t, "a = 1\n\t\tAND (\n\t\t\tb IS NULL\n\t\t\tOR b = 2\n\t\t)", got)
	})

	T.Run("nothing in, nothing out", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", joinPredicates(nil, "\t"))
	})
}
