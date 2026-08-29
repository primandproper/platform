package querygen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/filtering"

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

func TestGenerator_SetCondition(T *testing.T) {
	T.Parallel()

	T.Run("postgres binds the whole set as one array argument", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "widgets.id = ANY(sqlc.arg(ids)::text[])", pg().SetCondition("widgets.id", IDsArg))
	})

	T.Run("the other two expand the set into placeholders", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			test.EqOp(t, "widgets.id IN (sqlc.slice(ids))",
				For(d).SetCondition("widgets.id", IDsArg), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("it is the predicate the batched read is keyed on", func(t *testing.T) {
		t.Parallel()

		// One rendering, so a statement a consumer writes out and one this
		// package emits cannot come to spell a bound set differently.
		read := pg().SetReadQuery("ListThings", "things", []string{"parent_id", "name"},
			Read{}, SetKey{Column: "parent_id", Arg: "parent_ids"})

		test.StrContains(t, read.Content, pg().SetCondition("things.parent_id", "parent_ids"))
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

		_ = pg().SetCondition("widgets.id", "ids; DROP TABLE widgets")
	})
}

func TestGenerator_MatchConditions(T *testing.T) {
	T.Parallel()

	T.Run("renders one predicate per match, qualified", func(t *testing.T) {
		t.Parallel()

		got := pg().MatchConditions("things",
			Match{Column: "scope"},
			Match{Column: "status", Arg: "current_status"})

		test.Eq(t, []string{
			"things.scope = sqlc.arg(scope)",
			"things.status = sqlc.arg(current_status)",
		}, got)
	})

	T.Run("it is the predicate the keyed statements are rendered from", func(t *testing.T) {
		t.Parallel()

		// One rendering, so a statement a consumer writes out and one this
		// package emits cannot come to spell a narrowing differently.
		match := Match{Column: "owner", Against: OptionalNarrowing}

		list := pg().ListQueries("ListThings", "things", []string{IDColumn, "owner"}, match)

		test.StrContains(t, list[0].Content, pg().MatchConditions("things", match)[0])
	})

	T.Run("the optional narrowing carries each dialect's cast", func(t *testing.T) {
		t.Parallel()

		match := Match{Column: "owner", Against: OptionalNarrowing}

		// The NULL arm is where the three engines part company, and it is the
		// reason this fragment is exported at all: an authored statement that
		// spelled its own would be a second copy of Generator.unsetArgument.
		test.EqOp(t, "(sqlc.narg(owner)::text IS NULL OR things.owner = sqlc.narg(owner))",
			pg().MatchConditions("things", match)[0])
		test.EqOp(t, "(CAST(sqlc.narg(owner) AS CHAR) IS NULL OR things.owner = sqlc.narg(owner))",
			For(dialect.MySQL).MatchConditions("things", match)[0])
		test.EqOp(t, "(CAST(sqlc.narg(owner) AS TEXT) IS NULL OR things.owner = sqlc.narg(owner))",
			For(dialect.SQLite).MatchConditions("things", match)[0])
	})

	T.Run("it refuses a table name that is not an identifier", func(t *testing.T) {
		t.Parallel()

		defer func() {
			raised, ok := recover().(error)
			test.True(t, ok)
			test.ErrorIs(t, raised, dialect.ErrInvalidIdentifier)
		}()

		_ = pg().MatchConditions("things; DROP TABLE widgets", Match{Column: "scope"})
	})
}

func TestGenerator_WindowConditions(T *testing.T) {
	T.Parallel()

	T.Run("renders both halves against the named column", func(t *testing.T) {
		t.Parallel()

		got := pg().WindowConditions("entries.recorded_at", CreatedAfterArg, CreatedBeforeArg)

		test.Eq(t, []string{
			"entries.recorded_at > COALESCE(sqlc.narg(created_after), (SELECT CURRENT_TIMESTAMP - '999 years'::INTERVAL))",
			"entries.recorded_at < COALESCE(sqlc.narg(created_before), (SELECT CURRENT_TIMESTAMP + '999 years'::INTERVAL))",
		}, got)
	})

	T.Run("it is the window a derived list already renders", func(t *testing.T) {
		t.Parallel()

		// One rendering, so a table whose time column is its own and a table
		// following the convention cannot come to disagree about what an unset
		// bound means.
		derived := pg().FilterConditions("things", []string{IDColumn, CreatedAtColumn}, Ascending)

		for _, condition := range pg().WindowConditions("things."+CreatedAtColumn, CreatedAfterArg, CreatedBeforeArg) {
			test.StrContains(t, derived, condition)
		}
	})

	T.Run("the argument names are the caller's", func(t *testing.T) {
		t.Parallel()

		// A window whose bounds are two parameters of a method rather than a
		// filter's fields names something a reader of that method recognizes.
		got := pg().WindowConditions("entries.recorded_at", "recorded_after", "recorded_before")

		test.StrContains(t, got[0], "sqlc.narg(recorded_after)")
		test.StrContains(t, got[1], "sqlc.narg(recorded_before)")
	})

	T.Run("each dialect spells its own horizon", func(t *testing.T) {
		t.Parallel()

		test.StrContains(t, For(dialect.MySQL).WindowConditions("t.at", CreatedAfterArg, CreatedBeforeArg)[0],
			"INTERVAL 999 YEAR")
		test.StrContains(t, For(dialect.SQLite).WindowConditions("t.at", CreatedAfterArg, CreatedBeforeArg)[0],
			"datetime(CURRENT_TIMESTAMP, '-999 years')")
	})

	T.Run("it refuses an argument that is not an identifier", func(t *testing.T) {
		t.Parallel()

		defer func() {
			raised, ok := recover().(error)
			test.True(t, ok)
			test.ErrorIs(t, raised, dialect.ErrInvalidIdentifier)
		}()

		_ = pg().WindowConditions("t.at", CreatedAfterArg, "before; DROP TABLE widgets")
	})
}

func TestGenerator_LimitClause(T *testing.T) {
	T.Parallel()

	T.Run("postgres and sqlite default an absent page size", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			test.EqOp(t, fmt.Sprintf("LIMIT COALESCE(sqlc.narg(result_limit), %d)", filtering.DefaultQueryFilterLimit),
				For(d).LimitClause(), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("mysql takes a bare placeholder", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "LIMIT ?", For(dialect.MySQL).LimitClause())
	})

	T.Run("it is the clause a keyset walk ends with", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			g := For(d)

			test.True(t, strings.HasSuffix(g.CursorLimitClause("widgets", Ascending), g.LimitClause()),
				test.Sprintf("dialect %q", d))
		}
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
