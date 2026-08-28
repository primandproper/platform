package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// TestOptionalNarrowing covers the comparand a filter a caller may leave off is
// written with. What separates it from OptionalArgument is the meaning of an
// absent argument, and getting that backwards produces a query that runs.
func TestOptionalNarrowing(T *testing.T) {
	T.Parallel()

	T.Run("an absent argument narrows nothing", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.GetQuery("GetGadget", keyedTable, keyedColumns(),
				Match{Column: "name", Against: OptionalNarrowing})

			// The disjunction is the whole shape: the argument was not
			// supplied, or the column equals it.
			test.StrContains(t, q.Content,
				"("+g.unsetArgument("name")+" OR "+Qualify(keyedTable, "name")+" = sqlc.narg(name))",
				test.Sprintf("dialect %q", d))
		}
	})

	// The COALESCE spelling is the one that is semantically identical and
	// unplannable, so the property worth pinning is that the column appears on
	// one side of the comparison only.
	T.Run("compares the column against the argument rather than against itself", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			q := For(d).GetQuery("GetGadget", keyedTable, keyedColumns(),
				Match{Column: "name", Against: OptionalNarrowing})

			test.False(t, strings.Contains(q.Content, "COALESCE(sqlc.narg(name)"),
				test.Sprintf("dialect %q renders the unplannable COALESCE form", d))
		}
	})

	T.Run("inverts into the same disjunction", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.GetQuery("GetGadget", keyedTable, keyedColumns(),
				Match{Column: "name", Against: OptionalNarrowing, Exclude: true})

			test.StrContains(t, q.Content,
				"("+g.unsetArgument("name")+" OR "+Qualify(keyedTable, "name")+" <> sqlc.narg(name))",
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("binds under the name the match gives it", func(t *testing.T) {
		t.Parallel()

		q := For(dialect.Postgres).GetQuery("GetGadget", keyedTable, keyedColumns(),
			Match{Column: "name", Arg: "wanted_name", Against: OptionalNarrowing})

		test.StrContains(t, q.Content, "sqlc.narg(wanted_name)")
		test.False(t, strings.Contains(q.Content, "sqlc.narg(name)"))
	})

	// Only the NULL test is cast, and the equality must not be: on MySQL a cast
	// parameter carries the connection's collation instead of the column's,
	// which is error 1267 rather than a fallback.
	T.Run("casts the null test and nothing else", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)
			q := g.GetQuery("GetGadget", keyedTable, keyedColumns(),
				Match{Column: "name", Against: OptionalNarrowing})

			test.EqOp(t, 1, strings.Count(q.Content, "IS NULL OR"), test.Sprintf("dialect %q", d))
			test.StrContains(t, q.Content, "= sqlc.narg(name))", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("reports the comparand", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "an optional narrowing", OptionalNarrowing.String())
	})
}

// TestGenerator_SetListQueries covers the paged list narrowed by a bound set,
// which is the read a filter over a closed domain wants.
func TestGenerator_SetListQueries(T *testing.T) {
	T.Parallel()

	T.Run("emits both directions under one name", func(t *testing.T) {
		t.Parallel()

		queries := For(dialect.Postgres).SetListQueries("ListGadgets", keyedTable, keyedColumns(),
			SetKey{Column: "name", Arg: "names"})

		must.SliceLen(t, 2, queries)
		test.EqOp(t, "ListGadgets", queries[0].Annotation.Name)
		test.EqOp(t, DescendingName("ListGadgets"), queries[1].Annotation.Name)
		test.EqOp(t, ManyType, queries[0].Annotation.Type)
		test.EqOp(t, ManyType, queries[1].Annotation.Type)
	})

	// The set is the only predicate a list carries that is not a Match, and it
	// has to reach the counts as well as the page — a page filtered by state
	// beside a total counting every state is a pair of numbers that cannot both
	// be right.
	T.Run("carries the set into both counts", func(t *testing.T) {
		t.Parallel()

		g := For(dialect.Postgres)
		queries := g.SetListQueries("ListGadgets", keyedTable, keyedColumns(),
			SetKey{Column: "name", Arg: "names"})

		predicate := g.setPredicate(Qualify(keyedTable, "name"), "names")

		for _, q := range queries {
			test.EqOp(t, 3, strings.Count(q.Content, predicate),
				test.Sprintf("query %q", q.Annotation.Name))
		}
	})

	T.Run("renders the set after every match", func(t *testing.T) {
		t.Parallel()

		g := For(dialect.Postgres)
		queries := g.SetListQueries("ListGadgets", keyedTable, keyedColumns(),
			SetKey{Column: "name", Arg: "names"},
			Match{Column: BelongsToAccountColumn})

		set := strings.Index(queries[0].Content, g.setPredicate(Qualify(keyedTable, "name"), "names"))
		match := strings.Index(queries[0].Content, g.equalityPredicate(keyedTable, BelongsToAccountColumn, true))

		must.Greater(t, -1, set)
		must.Greater(t, -1, match)
		test.Greater(t, match, set)
	})

	T.Run("filters and pages exactly as an unkeyed list does", func(t *testing.T) {
		t.Parallel()

		g := For(dialect.Postgres)
		withSet := g.SetListQueries("ListGadgets", keyedTable, keyedColumns(), SetKey{Column: "name", Arg: "names"})
		without := g.ListQueries("ListGadgets", keyedTable, keyedColumns())

		predicate := "\n\tAND " + g.setPredicate(Qualify(keyedTable, "name"), "names")
		indented := "\n\t\t\tAND " + g.setPredicate(Qualify(keyedTable, "name"), "names")

		for i := range withSet {
			stripped := strings.ReplaceAll(withSet[i].Content, indented, "")
			stripped = strings.ReplaceAll(stripped, predicate, "")

			test.EqOp(t, without[i].Content, stripped)
		}
	})

	// A three-dialect consumer gets a panic rather than a statement whose
	// placeholders no argument list matches.
	T.Run("refuses the dialects that expand a set positionally", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			must.ErrorIs(t, recovered(func() {
				For(d).SetListQueries("ListGadgets", keyedTable, keyedColumns(), SetKey{Column: "name"})
			}), ErrPositionalSetInList, must.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses a set with no column", func(t *testing.T) {
		t.Parallel()

		must.ErrorIs(t, recovered(func() {
			For(dialect.Postgres).SetListQueries("ListGadgets", keyedTable, keyedColumns(), SetKey{})
		}), ErrMissingSetColumn)
	})

	T.Run("refuses an identifier it cannot interpolate", func(t *testing.T) {
		t.Parallel()

		must.ErrorIs(t, recovered(func() {
			For(dialect.Postgres).SetListQueries("ListGadgets", keyedTable, keyedColumns(),
				SetKey{Column: "name; DROP TABLE gadgets"})
		}), dialect.ErrInvalidIdentifier)
	})
}
