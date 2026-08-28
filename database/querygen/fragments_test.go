package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
)

// The assertions below pin Postgres, which is the dialect whose output existed
// first and is therefore the one a regression would be least visible in.
// generator_test.go pins what the other two do differently, and
// containers_test.go runs all three against real servers.
func pg() *Generator { return For(dialect.Postgres) }

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

func TestGenerator_ContainsCondition(T *testing.T) {
	T.Parallel()

	T.Run("binds the term and supplies the wildcards itself", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t,
			`things.name ILIKE '%' || sqlc.arg(name_query)::text || '%'`,
			pg().ContainsCondition("things.name", "name_query"))
	})
}

func TestGenerator_CursorCondition(T *testing.T) {
	T.Parallel()

	T.Run("an absent cursor coalesces rather than branching", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "things.id > COALESCE(sqlc.narg(page_cursor), '')", pg().CursorCondition("things", Ascending))
	})

	// The descending half reaches for no sentinel, because there is no string
	// that sorts above every id in every collation — it coalesces to the row's
	// own key instead. See CursorCondition.
	T.Run("the descending walk coalesces to the row's own key", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t,
			"(things.id <= COALESCE(sqlc.narg(page_cursor), things.id) AND things.id <> COALESCE(sqlc.narg(page_cursor), ''))",
			pg().CursorCondition("things", Descending))
	})

	T.Run("both walks read an empty cursor as the first page", func(t *testing.T) {
		t.Parallel()

		// The two directions say it differently and have to mean the same
		// thing: a store that switched direction mid-walk would otherwise find
		// the first page of one of them empty. Both spell the absent cursor as
		// the empty string, and neither reads the argument except beside the
		// column it filters — which is what keeps every analyzer able to type
		// it.
		for _, d := range everyDialect() {
			for _, direction := range []Direction{Ascending, Descending} {
				test.StrContains(t, For(d).CursorCondition("things", direction),
					"COALESCE(sqlc.narg(page_cursor), '')",
					test.Sprintf("dialect %q, %s", d, direction))
			}
		}
	})
}

func TestGenerator_CursorLimitClause(T *testing.T) {
	T.Parallel()

	T.Run("orders by the column the cursor names", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "ORDER BY things.id ASC\nLIMIT COALESCE(sqlc.narg(result_limit), 50)", pg().CursorLimitClause("things", Ascending))
	})

	T.Run("orders the other way for the descending walk", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "ORDER BY things.id DESC\nLIMIT COALESCE(sqlc.narg(result_limit), 50)", pg().CursorLimitClause("things", Descending))
	})

	// The ordering is the same on every dialect; only the page size differs, and
	// only because MySQL's grammar admits no expression after LIMIT.
	T.Run("orders the same way on every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			test.StrContains(t, For(d).CursorLimitClause("things", Ascending), "ORDER BY things.id ASC\n",
				test.Sprintf("dialect %q", d))
			test.StrContains(t, For(d).CursorLimitClause("things", Descending), "ORDER BY things.id DESC\n",
				test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_CursorPaginationFragment(T *testing.T) {
	T.Parallel()

	T.Run("prefixes the predicate with AND, for the tail of a WHERE", func(t *testing.T) {
		t.Parallel()

		want := "AND things.id > COALESCE(sqlc.narg(page_cursor), '')\n" +
			"ORDER BY things.id ASC\nLIMIT COALESCE(sqlc.narg(result_limit), 50)"

		test.EqOp(t, want, pg().CursorPaginationFragment("things", Ascending))
	})

	// The predicate and the ordering come from one direction, which is the
	// property that keeps a cursor naming a position in the order it is walked
	// in — see cursorLimitClause.
	T.Run("the comparison and the ordering agree in both directions", func(t *testing.T) {
		t.Parallel()

		want := "AND (things.id <= COALESCE(sqlc.narg(page_cursor), things.id) AND things.id <> COALESCE(sqlc.narg(page_cursor), ''))\n" +
			"ORDER BY things.id DESC\nLIMIT COALESCE(sqlc.narg(result_limit), 50)"

		test.EqOp(t, want, pg().CursorPaginationFragment("things", Descending))
	})
}

func TestGenerator_ReindexScanQuery(T *testing.T) {
	T.Parallel()

	T.Run("walks ids in byte order", func(t *testing.T) {
		t.Parallel()

		want := `SELECT things.id
FROM things
WHERE things.archived_at IS NULL
	AND things.id COLLATE "C" > sqlc.arg(page_cursor)
ORDER BY things.id COLLATE "C"
LIMIT COALESCE(sqlc.narg(result_limit), 50);`

		test.EqOp(t, want, pg().ReindexScanQuery("things"))
	})

	T.Run("both the comparison and the ordering carry the byte order, on every dialect", func(t *testing.T) {
		t.Parallel()

		// Either one alone is the failure mode search/sync's pruner cannot
		// survive: a walk ordered one way and resumed the other skips rows, and
		// the pruner reads a skipped row as a document to delete. The wrapper
		// differs by dialect; that there are two of it does not.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			g := For(d)

			test.EqOp(t, 2, strings.Count(g.ReindexScanQuery("things"), g.byteOrdered("things.id")),
				test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_IndexStampQuery(T *testing.T) {
	T.Parallel()

	T.Run("stamps every id it is handed", func(t *testing.T) {
		t.Parallel()

		want := `UPDATE things SET
	last_indexed_at = CURRENT_TIMESTAMP
WHERE id = ANY(sqlc.arg(ids)::text[]);`

		test.EqOp(t, want, pg().IndexStampQuery("things"))
	})

	T.Run("is unscoped by owner and by archival, on every dialect", func(t *testing.T) {
		t.Parallel()

		// Both omissions are the search sync servicing itself. A predicate here
		// would either exclude rows the index accepted or make the statement
		// unemittable for a table with no soft delete.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			got := For(d).IndexStampQuery("things")

			test.StrNotContains(t, got, ArchivedAtColumn, test.Sprintf("dialect %q", d))
			test.StrNotContains(t, got, BelongsToAccountColumn, test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_FilterConditions(T *testing.T) {
	T.Parallel()

	T.Run("renders the whole window for a conventional table", func(t *testing.T) {
		t.Parallel()

		want := `things.created_at > COALESCE(sqlc.narg(created_after), (SELECT CURRENT_TIMESTAMP - '999 years'::INTERVAL))
	AND things.created_at < COALESCE(sqlc.narg(created_before), (SELECT CURRENT_TIMESTAMP + '999 years'::INTERVAL))
	AND (
		things.last_updated_at IS NULL
		OR things.last_updated_at > COALESCE(sqlc.narg(updated_after), (SELECT CURRENT_TIMESTAMP - '999 years'::INTERVAL))
	)
	AND (
		things.last_updated_at IS NULL
		OR things.last_updated_at < COALESCE(sqlc.narg(updated_before), (SELECT CURRENT_TIMESTAMP + '999 years'::INTERVAL))
	)
	AND (COALESCE(sqlc.narg(include_archived), false)::boolean OR things.archived_at IS NULL)
	AND things.id > COALESCE(sqlc.narg(page_cursor), '')`

		test.EqOp(t, want, pg().FilterConditions("things", columnsFor(), Ascending))
	})

	T.Run("is the whole clause, so include_archived is the only thing deciding", func(t *testing.T) {
		t.Parallel()

		// A bare archived_at IS NULL anywhere in the clause would make the flag
		// decorative: the rows it admits would already have been excluded. True
		// on every dialect, since only the flag's spelling differs.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			got := For(d).FilterConditions("things", columnsFor(), Ascending)

			test.EqOp(t, 1, strings.Count(got, ArchivedAtColumn+" IS NULL"), test.Sprintf("dialect %q", d))
			test.StrContains(t, got, "sqlc.narg("+IncludeArchivedArg+")", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("omits the predicates whose columns the table lacks", func(t *testing.T) {
		t.Parallel()

		got := pg().FilterConditions("things", []string{IDColumn, "name"}, Ascending)

		test.EqOp(t, "things.id > COALESCE(sqlc.narg(page_cursor), '')", got)
	})

	T.Run("places the caller's conditions before the cursor", func(t *testing.T) {
		t.Parallel()

		got := pg().FilterConditions("things", []string{IDColumn}, Ascending, "things.kind = sqlc.arg(kind)")

		test.EqOp(t, "things.kind = sqlc.arg(kind)\n\tAND things.id > COALESCE(sqlc.narg(page_cursor), '')", got)
	})
}

func TestGenerator_FilterCountSelect(T *testing.T) {
	T.Parallel()

	T.Run("counts the filter window without the cursor", func(t *testing.T) {
		t.Parallel()

		got := pg().FilterCountSelect("things", columnsFor(), nil)

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
		WHERE things.created_at > COALESCE(sqlc.narg(created_after), (SELECT CURRENT_TIMESTAMP - '999 years'::INTERVAL))
			AND things.created_at < COALESCE(sqlc.narg(created_before), (SELECT CURRENT_TIMESTAMP + '999 years'::INTERVAL))
	) AS filtered_count`

		test.EqOp(t, want, pg().FilterCountSelect("things", []string{IDColumn, CreatedAtColumn}, nil))
	})

	T.Run("renders the joins it is given", func(t *testing.T) {
		t.Parallel()

		join := JoinStatement{JoinTarget: "stuff", TargetColumn: IDColumn, OnTable: "things", OnColumn: "stuff_id"}

		got := pg().FilterCountSelect("things", []string{IDColumn}, []string{join.String()})

		test.StrContains(t, got, "\n\t\tJOIN stuff ON things.stuff_id=stuff.id")
	})

	T.Run("omits a WHERE it would have nothing to put after", func(t *testing.T) {
		t.Parallel()

		got := pg().FilterCountSelect("things", []string{IDColumn, "name"}, nil)

		test.EqOp(t, "(\n\t\tSELECT COUNT(things.id)\n\t\tFROM things\n\t) AS filtered_count", got)
	})
}

func TestGenerator_TotalCountSelect(T *testing.T) {
	T.Parallel()

	T.Run("applies the same archived toggle as the filter", func(t *testing.T) {
		t.Parallel()

		want := `(
		SELECT COUNT(things.id)
		FROM things
		WHERE (COALESCE(sqlc.narg(include_archived), false)::boolean OR things.archived_at IS NULL)
	) AS total_count`

		test.EqOp(t, want, pg().TotalCountSelect("things", columnsFor(), nil))
	})

	T.Run("ignores the time window, which is what makes it the total", func(t *testing.T) {
		t.Parallel()

		got := pg().TotalCountSelect("things", columnsFor(), nil)

		test.StrNotContains(t, got, CreatedAfterArg)
		test.StrNotContains(t, got, UpdatedAfterArg)
		test.StrNotContains(t, got, CursorArg)
	})

	T.Run("keeps the caller's scoping conditions, which are not part of the window", func(t *testing.T) {
		t.Parallel()

		got := pg().TotalCountSelect("things", columnsFor(), nil, "things.belongs_to_account = sqlc.arg(belongs_to_account)")

		test.StrContains(t, got, "AND things.belongs_to_account = sqlc.arg(belongs_to_account)")
	})

	T.Run("omits a WHERE it would have nothing to put after", func(t *testing.T) {
		t.Parallel()

		got := pg().TotalCountSelect("things", []string{IDColumn}, nil)

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

// The three fragments an authored statement borrows. Each is the same rendering
// the emitted statements use, exported so that a corpus writing a statement out
// by hand cannot arrive at a second opinion about it — see the methods' own
// comments for what a second opinion costs on each.

func TestGenerator_SetPredicate(T *testing.T) {
	T.Parallel()

	T.Run("Postgres binds the whole set as one array", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "things.id = ANY(sqlc.arg(ids)::text[])", pg().SetPredicate("things.id", IDsArg))
	})

	// The other two have no array type, so the set is an expansion sqlc writes.
	T.Run("the other two expand a placeholder per element", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			test.EqOp(t, "things.id IN (sqlc.slice(ids))", For(d).SetPredicate("things.id", IDsArg),
				test.Sprintf("dialect %q", d))
		}
	})

	// The column may be qualified, because a statement joining more than one
	// table has to qualify it. Only the argument is an identifier this package
	// promises to have checked.
	T.Run("it refuses an argument that is not an identifier", func(t *testing.T) {
		t.Parallel()

		defer func() {
			raised, ok := recover().(error)
			test.True(t, ok)
			test.ErrorIs(t, raised, dialect.ErrInvalidIdentifier)
		}()

		_ = pg().SetPredicate("things.id", "ids; DROP TABLE things")
	})
}

func TestGenerator_StoredNow(T *testing.T) {
	T.Parallel()

	// MySQL's bare CURRENT_TIMESTAMP is second-granular whatever the column
	// declares, and an update that writes the value a row already holds reports
	// zero rows changed there. The fractional form is what keeps a correct write
	// from reading as a missing row.
	T.Run("MySQL asks for the fractional form", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "CURRENT_TIMESTAMP(6)", For(dialect.MySQL).StoredNow())
	})

	T.Run("the other two need nothing", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			test.EqOp(t, NowExpression, For(d).StoredNow(), test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_LimitClause(T *testing.T) {
	T.Parallel()

	// MySQL takes an integer literal or a bare placeholder after LIMIT and
	// nothing else: a named argument reference there is a parse error rather
	// than a slower plan.
	T.Run("MySQL takes a bare marker", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "LIMIT ?", For(dialect.MySQL).LimitClause())
	})

	T.Run("the other two default an absent page size", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			clause := For(d).LimitClause()

			test.True(t, strings.HasPrefix(clause, "LIMIT COALESCE(sqlc.narg("+LimitArg+")"),
				test.Sprintf("dialect %q rendered %q", d, clause))
		}
	})
}
