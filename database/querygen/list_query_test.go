package querygen

import (
	"slices"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/filtering"

	"github.com/shoenig/test"
)

// TestGenerator_ListQuery pins the property that is ListQuery's whole reason to
// exist: it is listStatement with its matches where WithOwnership's column goes,
// so a keyed page filters, counts and walks its cursor exactly as an unkeyed one
// does. What a keyed list can get wrong is a predicate that reaches the outer
// read and not the counts beside it, and an argument that repeats.
func TestGenerator_ListQuery(T *testing.T) {
	T.Parallel()

	T.Run("is annotated as the page of rows it returns", func(t *testing.T) {
		t.Parallel()

		columns := []string{IDColumn, "owner", "name", CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn}

		q := For(dialectForContent()).ListQuery("ListWidgetsByOwner", "widgets", columns, Match{Column: "owner"})

		test.EqOp(t, "ListWidgetsByOwner", q.Annotation.Name)
		test.EqOp(t, ManyType, q.Annotation.Type)

		// The canonical spelling: sqlc argument references, no bind markers.
		test.StrContains(t, q.Content, "sqlc.arg(owner)")
		test.StrNotContains(t, q.Content, "$1")
	})

	T.Run("binds a repeated argument once on Postgres and once per occurrence elsewhere", func(t *testing.T) {
		t.Parallel()

		// created_after is rendered into the SELECT's WHERE and again into the
		// filtered count beside it, and include_archived into both counts as
		// well: the repeat is the ordinary case here, not the exotic one.
		//
		// This is the assertion the SQLite arm needs. Its placeholder is a bare
		// '?' like MySQL's, so treating it as numbered renders a marker per
		// occurrence while reporting one argument for all of them, and every
		// value after the first lands in the wrong slot.
		for _, d := range everyDialect() {
			sql, args := bindQuery(d, For(d).ListQuery("ListGadgets", keyedTable, keyedColumns()))

			assertMarkersMatchArgs(t, d, sql, args)

			occurrences := 0

			for _, name := range args {
				if name == CreatedAfterArg {
					occurrences++
				}
			}

			want := 2
			if d == dialect.Postgres {
				want = 1
			}

			test.EqOp(t, want, occurrences, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("counts what is left rather than what matches", func(t *testing.T) {
		t.Parallel()

		// filtered_count carries the window and the archived toggle and not the
		// cursor, so it does not shrink as a caller pages. The cursor argument
		// appears once, in the outer WHERE.
		for _, d := range everyDialect() {
			sql, args := bindQuery(d, For(d).ListQuery("ListGadgets", keyedTable, keyedColumns()))

			cursors := 0

			for _, name := range args {
				if name == CursorArg {
					cursors++
				}
			}

			test.EqOp(t, 1, cursors, test.Sprintf("dialect %q", d))
			test.StrContains(t, sql, "AS filtered_count", test.Sprintf("dialect %q", d))
			test.StrContains(t, sql, "AS total_count", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("renders every match into the counts as well as the outer read", func(t *testing.T) {
		t.Parallel()

		// A keyed list whose counts are unkeyed reports the whole table's
		// totals beside one owner's page, which reads as a pagination bug
		// somewhere else entirely.
		for _, d := range everyDialect() {
			sql, args := bindQuery(d, For(d).ListQuery("ListGadgets", keyedTable, keyedColumns(),
				Match{Column: BelongsToAccountColumn}, Match{Column: "name"}))

			test.EqOp(t, 3, strings.Count(sql, Qualify(keyedTable, BelongsToAccountColumn)+" ="),
				test.Sprintf("dialect %q", d))
			test.EqOp(t, 3, strings.Count(sql, Qualify(keyedTable, "name")+" ="),
				test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, sql, args)
		}
	})
}

func TestListArgumentsAreTheOnesFilteringBinds(T *testing.T) {
	T.Parallel()

	T.Run("a list statement names every filter argument and no others", func(t *testing.T) {
		t.Parallel()

		// This is the tie between the two halves of a filtered read: the
		// statements emitted here name the arguments, and filtering names the
		// values they take. The names are shared — the Arg constants in this
		// package are aliases of filtering's — so a mismatch cannot be a
		// spelling; it is a window argument that reached one half and not the
		// other.
		//
		// Either direction is silent at runtime. An argument the SQL names and
		// nothing binds is an unbound placeholder, which at least fails loudly
		// on Postgres and quietly binds the next value along on the positional
		// dialects. An argument bound under a name no statement mentions binds
		// nothing and filters nothing, which is what a filter nobody set looks
		// like.
		expected := slices.Sorted(slices.Values([]string{
			filtering.ArgCreatedAfter,
			filtering.ArgCreatedBefore,
			filtering.ArgCursor,
			filtering.ArgIncludeArchived,
			filtering.ArgResultLimit,
			filtering.ArgUpdatedAfter,
			filtering.ArgUpdatedBefore,
		}))

		for _, d := range everyDialect() {
			// No ownership column and no matches, so what is left is the
			// filter's own vocabulary. The list is deduplicated because the
			// positional dialects repeat a name once per occurrence.
			_, args := bindQuery(d, For(d).ListQuery("ListGadgets", keyedTable, keyedColumns()))

			names := slices.Sorted(slices.Values(args))
			names = slices.Compact(names)

			test.Eq(t, expected, names, test.Sprintf("dialect %q", d))
		}
	})
}
