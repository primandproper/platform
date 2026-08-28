package querygen

import (
	"slices"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/filtering"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestGenerator_ListQueries pins the pair that is ListQueries' whole reason to
// exist: one call emits both directions of a paged list, named and annotated
// for a query file, so a corpus cannot carry only the direction somebody
// happened to think of.
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
				test.StrNotContains(t, q.Content, "$1")

				// And a statement the harness can hand to a driver: every
				// marker accounted for by a named argument.
				sql, args := bindArguments(d, q.Content)
				assertMarkersMatchArgs(t, d, sql, args)
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

// TestGenerator_ListQueries_KeyedPaging pins what a keyed page shares with an
// unkeyed one: it is listStatement with its matches where WithOwnership's
// column goes, so a keyed page filters, counts and walks its cursor exactly as
// an unkeyed one does. What a keyed list can get wrong is a predicate that
// reaches the outer read and not the counts beside it, and an argument that
// repeats. Every assertion runs against both directions, because both are
// executed.
func TestGenerator_ListQueries_KeyedPaging(T *testing.T) {
	T.Parallel()

	directions := []Direction{Ascending, Descending}

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
			for _, direction := range directions {
				queries := For(d).ListQueries("ListGadgets", keyedTable, keyedColumns())
				sql, args := bindQuery(d, pagedList(queries, direction))

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

				test.EqOp(t, want, occurrences, test.Sprintf("dialect %q direction %q", d, direction))
			}
		}
	})

	T.Run("counts what is left rather than what matches", func(t *testing.T) {
		t.Parallel()

		// filtered_count carries the window and the archived toggle and not the
		// cursor, so it does not shrink as a caller pages. The cursor reference
		// lives in the outer WHERE alone — once on the ascending walk, and
		// twice on the descending one, whose predicate compares the column
		// against the cursor and against the cursor's absence. Postgres numbers
		// its markers and binds it once either way; the positional dialects
		// take a value per occurrence.
		for _, d := range everyDialect() {
			for _, direction := range directions {
				queries := For(d).ListQueries("ListGadgets", keyedTable, keyedColumns())
				sql, args := bindQuery(d, pagedList(queries, direction))

				cursors := 0

				for _, name := range args {
					if name == CursorArg {
						cursors++
					}
				}

				want := 1
				if direction == Descending && d != dialect.Postgres {
					want = 2
				}

				test.EqOp(t, want, cursors, test.Sprintf("dialect %q direction %q", d, direction))
				test.StrContains(t, sql, "AS filtered_count", test.Sprintf("dialect %q direction %q", d, direction))
				test.StrContains(t, sql, "AS total_count", test.Sprintf("dialect %q direction %q", d, direction))
			}
		}
	})

	T.Run("renders every match into the counts as well as the outer read", func(t *testing.T) {
		t.Parallel()

		// A keyed list whose counts are unkeyed reports the whole table's
		// totals beside one owner's page, which reads as a pagination bug
		// somewhere else entirely.
		for _, d := range everyDialect() {
			for _, direction := range directions {
				queries := For(d).ListQueries("ListGadgets", keyedTable, keyedColumns(),
					Match{Column: BelongsToAccountColumn}, Match{Column: "name"})
				sql, args := bindQuery(d, pagedList(queries, direction))

				test.EqOp(t, 3, strings.Count(sql, Qualify(keyedTable, BelongsToAccountColumn)+" ="),
					test.Sprintf("dialect %q direction %q", d, direction))
				test.EqOp(t, 3, strings.Count(sql, Qualify(keyedTable, "name")+" ="),
					test.Sprintf("dialect %q direction %q", d, direction))
				assertMarkersMatchArgs(t, d, sql, args)
			}
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
			for _, direction := range []Direction{Ascending, Descending} {
				// No ownership column and no matches, so what is left is the
				// filter's own vocabulary. The list is deduplicated because the
				// positional dialects repeat a name once per occurrence.
				queries := For(d).ListQueries("ListGadgets", keyedTable, keyedColumns())
				_, args := bindQuery(d, pagedList(queries, direction))

				names := slices.Sorted(slices.Values(args))
				names = slices.Compact(names)

				test.Eq(t, expected, names, test.Sprintf("dialect %q direction %q", d, direction))
			}
		}
	})
}
