package querygen

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The claim this file exists to check is that there is no second rendering: a
// bound statement is the emitted one with its argument references rewritten, and
// nothing else about it moved.
//
// Most of that claim is now structural rather than assertable. Every Bound*
// method calls the same statement function StandardCRUD calls and hands the
// result to bindArguments, so "the bound get is the emitted get" is true by
// construction and a test asserting it would only be restating the call graph.
//
// What is left to assert is what the rewrite can actually get wrong, and there
// are three things. An argument reference in a spelling the pattern does not
// match survives into the statement a driver is handed — see
// TestBoundStatements. A marker can be numbered in a way that disagrees with the
// argument list, which the dialects disagree about and assertMarkersMatchArgs
// pins. And the arguments a statement needs have to be the ones a caller can
// supply, which is BindFilter's half.

// boundTable is the table the bound statements below are rendered against. It
// is not the container suite's widgets: nothing here executes, so the DDL is
// beside the point and a distinct name keeps the two files' fixtures from
// looking like one.
const boundTable = "gadgets"

// boundColumns is a conventional table's column set — every column this package
// has an opinion about, so no predicate is skipped for want of one.
func boundColumns() []string {
	return []string{
		IDColumn,
		"name",
		BelongsToAccountColumn,
		LastIndexedAtColumn,
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

// placeholder matches either dialect family's bind marker. Neither character
// appears in the emitted SQL outside one, so counting matches counts
// placeholders.
var placeholder = regexp.MustCompile(`\$\d+|\?`)

// assertMarkersMatchArgs checks the invariant a driver enforces at
// execution time: every marker in the statement has a value, and every value has
// a marker.
//
// The two schemes satisfy it differently, which is the reason it is worth
// asserting at all rather than counting markers. A positional marker consumes
// the next value, so the count of them is the count of arguments. A numbered one
// names its value, so a repeat reuses an ordinal and there are more markers than
// arguments — what has to hold there is that the ordinals are exactly 1..len,
// with none skipped: an ordinal past the end is a driver error, and a gap is an
// argument nothing reads while everything after it is off by one.
func assertMarkersMatchArgs(tb testing.TB, d dialect.Dialect, b Bound) {
	tb.Helper()

	markers := placeholder.FindAllString(b.SQL, -1)

	if d != dialect.Postgres {
		test.EqOp(tb, len(markers), len(b.Args),
			test.Sprintf("dialect %q: markers and arguments disagree in\n%s", d, b.SQL))

		return
	}

	seen := make(map[int]bool, len(b.Args))

	for _, marker := range markers {
		n, err := strconv.Atoi(strings.TrimPrefix(marker, "$"))
		must.NoError(tb, err, must.Sprintf("dialect %q: marker %q", d, marker))
		seen[n] = true
	}

	test.MapLen(tb, len(b.Args), seen, test.Sprintf("dialect %q: in\n%s", d, b.SQL))

	for n := 1; n <= len(b.Args); n++ {
		test.True(tb, seen[n], test.Sprintf("dialect %q: no $%d in\n%s", d, n, b.SQL))
	}
}

func TestGenerator_BoundGet(T *testing.T) {
	T.Parallel()

	T.Run("keys on the extra match columns as well as the id", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got := For(d).BoundGet(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn})

			test.Eq(t, []string{IDColumn, BelongsToAccountColumn}, got.Args, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, Qualify(boundTable, BelongsToAccountColumn)+" =", test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, got)
		}
	})

	T.Run("excludes archived rows outright", func(t *testing.T) {
		t.Parallel()

		// The single-row reads do not carry the include_archived toggle: a
		// caller wanting an archived row wants a different statement. Losing
		// this predicate is invisible until something reads a row it archived.
		for _, d := range everyDialect() {
			test.StrContains(t, For(d).BoundGet(boundTable, boundColumns()).SQL,
				Qualify(boundTable, ArchivedAtColumn)+" IS NULL", test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_BoundCreate(T *testing.T) {
	T.Parallel()

	T.Run("supplies no value for the database-owned columns", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got := For(d).BoundCreate(boundTable, ForInsert(boundColumns()), nil)

			for _, column := range []string{CreatedAtColumn, LastUpdatedAtColumn, ArchivedAtColumn, LastIndexedAtColumn} {
				test.SliceNotContains(t, got.Args, column, test.Sprintf("dialect %q, column %q", d, column))
			}
		}
	})
}

func TestGenerator_BoundUpdate(T *testing.T) {
	T.Parallel()

	T.Run("assigns before it keys, so the argument order is the assignments then the predicates", func(t *testing.T) {
		t.Parallel()

		updates := ForUpdate(boundColumns(), BelongsToAccountColumn)

		for _, d := range everyDialect() {
			got := For(d).BoundUpdate(boundTable, boundColumns(), updates, nil, Match{Column: BelongsToAccountColumn})

			test.Eq(t, append(slices.Clone(updates), IDColumn, BelongsToAccountColumn), got.Args,
				test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, got)
		}
	})

	T.Run("a column that is both assigned and matched is one argument", func(t *testing.T) {
		t.Parallel()

		// Which is a statement that sets a column to the value it is being
		// required to already hold — legal, useless, and the sqlc path's
		// behavior too, since WithOwnership renders the owner into the SET and
		// the WHERE from the same argument name. It is named here so that the
		// argument list stops being a surprise: a caller wanting to move a row
		// between owners guards the move with Match.Arg, and one wanting not to
		// assign the column at all keeps it out of the updatable set, which is
		// what ForUpdate's exceptions are for.
		got := For(dialect.Postgres).BoundUpdate(boundTable, boundColumns(), ForUpdate(boundColumns()), nil,
			Match{Column: BelongsToAccountColumn})

		test.SliceContains(t, got.Args, BelongsToAccountColumn)
		test.EqOp(t, 1, strings.Count(strings.Join(got.Args, " "), BelongsToAccountColumn))

		// Both occurrences reuse the one ordinal, so Bind hands the driver one
		// value for the two of them.
		values := map[string]any{}
		for _, name := range got.Args {
			values[name] = name
		}

		bound, err := got.Bind(values)
		must.NoError(t, err)
		test.SliceLen(t, len(got.Args), bound)
	})

	T.Run("a guard on an assigned column binds under its own argument name", func(t *testing.T) {
		t.Parallel()

		// The transfer: the new owner in the SET, the current one in the WHERE,
		// and two arguments rather than one — which is the difference between a
		// statement two concurrent transfers race for and one that assigns the
		// owner it just required the row to already have.
		const current = "current_" + BelongsToAccountColumn

		for _, d := range everyDialect() {
			got := For(d).BoundUpdate(boundTable, boundColumns(), []string{BelongsToAccountColumn}, nil,
				Match{Column: BelongsToAccountColumn, Arg: current})

			test.Eq(t, []string{BelongsToAccountColumn, IDColumn, current}, got.Args,
				test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, BelongsToAccountColumn+" = ", test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, got)
		}
	})
}

func TestMatch_Arg(T *testing.T) {
	T.Parallel()

	T.Run("defaults to the column, which is what every keyed read wants", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got := For(d).BoundGet(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn})

			test.SliceContains(t, got.Args, BelongsToAccountColumn, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("renames the argument without renaming the column", func(t *testing.T) {
		t.Parallel()

		// The predicate still names the column — a guard compares the row's own
		// column against something, and only the something is being named
		// differently.
		got := For(dialect.Postgres).BoundGet(boundTable, boundColumns(),
			Match{Column: BelongsToAccountColumn, Arg: "previous_account"})

		test.StrContains(t, got.SQL, Qualify(boundTable, BelongsToAccountColumn)+" = ")
		test.Eq(t, []string{IDColumn, "previous_account"}, got.Args)
	})
}

func TestGenerator_BoundArchive(T *testing.T) {
	T.Parallel()

	T.Run("stamps rather than deletes, and only an unarchived row", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got := For(d).BoundArchive(boundTable, boundColumns())

			test.StrContains(t, got.SQL, "UPDATE "+boundTable, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, ArchivedAtColumn+" = "+NowExpression, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, got.SQL, "DELETE", test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_BoundList(T *testing.T) {
	T.Parallel()

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
			got := For(d).BoundList(boundTable, boundColumns())

			assertMarkersMatchArgs(t, d, got)

			occurrences := 0

			for _, name := range got.Args {
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
			got := For(d).BoundList(boundTable, boundColumns())

			cursors := 0

			for _, name := range got.Args {
				if name == CursorArg {
					cursors++
				}
			}

			test.EqOp(t, 1, cursors, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, "AS filtered_count", test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, "AS total_count", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("renders every match into the counts as well as the outer read", func(t *testing.T) {
		t.Parallel()

		// A keyed list whose counts are unkeyed reports the whole table's
		// totals beside one owner's page, which reads as a pagination bug
		// somewhere else entirely.
		for _, d := range everyDialect() {
			got := For(d).BoundList(boundTable, boundColumns(),
				Match{Column: BelongsToAccountColumn}, Match{Column: "name"})

			test.EqOp(t, 3, strings.Count(got.SQL, Qualify(boundTable, BelongsToAccountColumn)+" ="),
				test.Sprintf("dialect %q", d))
			test.EqOp(t, 3, strings.Count(got.SQL, Qualify(boundTable, "name")+" ="),
				test.Sprintf("dialect %q", d))
			assertMarkersMatchArgs(t, d, got)
		}
	})
}

func TestBindArguments(T *testing.T) {
	T.Parallel()

	T.Run("refuses a set, whose arity is not known until the values are", func(t *testing.T) {
		t.Parallel()

		// sqlc.slice is a macro sqlc expands per call, because the arity
		// belongs to the values. A statement carrying one has no single
		// executable rendering, and a Bound* method written around one wants
		// BoundIDSet instead. idSetPredicate is the only fragment that renders
		// one, and only off Postgres.
		for _, d := range everyDialect() {
			err := recovered(func() { _, _ = bindArguments(d, For(dialect.MySQL).idSetPredicate()) })

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, ErrUnboundableStatement, test.Sprintf("dialect %q", d))
			test.StrContains(t, err.Error(), IDsArg, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("binds MySQL's unnamed limit marker under the page size", func(t *testing.T) {
		t.Parallel()

		// MySQL's grammar takes a bare placeholder after LIMIT and nothing
		// else, so limitClause writes the marker rather than a reference. It
		// still has to reach a caller as the page size, under the same key the
		// other two dialects name in the SQL itself.
		sql, args := bindArguments(dialect.MySQL, "SELECT 1 WHERE a = sqlc.arg(a)\n"+For(dialect.MySQL).limitClause())

		test.EqOp(t, "SELECT 1 WHERE a = ?\nLIMIT ?", sql)
		test.Eq(t, []string{"a", LimitArg}, args)
	})

	T.Run("refuses an unnamed marker on a dialect that does not emit one", func(t *testing.T) {
		t.Parallel()

		// Only MySQL's limit clause places a marker itself. A '?' anywhere in
		// a statement rendered for the other two came from a caller's hand,
		// and there is no name to bind it under — so it is the same class of
		// programming error as a set.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			err := recovered(func() { _, _ = bindArguments(d, "SELECT 1 LIMIT ?") })

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, ErrUnboundableStatement, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("numbers markers where they appear rather than where they were rendered", func(t *testing.T) {
		t.Parallel()

		// The property a spliced fragment depends on. One rendering of
		// sqlc.arg(a) can reach the statement three times, and each occurrence
		// has to be numbered at the position it occupies in the finished text.
		sql, args := bindArguments(dialect.Postgres, "A sqlc.arg(a) B sqlc.narg(b) C sqlc.arg(a)")

		test.EqOp(t, "A $1 B $2 C $1", sql)
		test.Eq(t, []string{"a", "b"}, args)

		sql, args = bindArguments(dialect.MySQL, "A sqlc.arg(a) B sqlc.narg(b) C sqlc.arg(a)")

		test.EqOp(t, "A ? B ? C ?", sql)
		test.Eq(t, []string{"a", "b", "a"}, args)
	})

	T.Run("treats the nullable form as the same argument", func(t *testing.T) {
		t.Parallel()

		// sqlc reads the distinction and generates a nullable Go field; a
		// driver does not, and binds whatever it was handed.
		sql, args := bindArguments(dialect.Postgres, "sqlc.arg(a) sqlc.narg(a)")

		test.EqOp(t, "$1 $1", sql)
		test.Eq(t, []string{"a"}, args)
	})

	T.Run("leaves a statement with no arguments alone", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			sql, args := bindArguments(d, "SELECT 1;")

			test.EqOp(t, "SELECT 1;", sql, test.Sprintf("dialect %q", d))
			test.SliceEmpty(t, args, test.Sprintf("dialect %q", d))
		}
	})
}

// boundStatements is every Bound a conventional table has, keyed by name.
func boundStatements(tb testing.TB, d dialect.Dialect) map[string]Bound {
	tb.Helper()

	var (
		g       = For(d)
		columns = boundColumns()
		owner   = Match{Column: BelongsToAccountColumn}
	)

	return map[string]Bound{
		"create":  g.BoundCreate(boundTable, ForInsert(columns), []string{"name"}),
		"get":     g.BoundGet(boundTable, columns, owner),
		"exists":  g.BoundExists(boundTable, columns, owner),
		"update":  g.BoundUpdate(boundTable, columns, ForUpdate(columns, BelongsToAccountColumn), nil, owner),
		"archive": g.BoundArchive(boundTable, columns, owner),
		"list":    g.BoundList(boundTable, columns, owner),
	}
}

func TestBoundStatements(T *testing.T) {
	T.Parallel()

	T.Run("carry no generator syntax into what a driver is handed", func(t *testing.T) {
		t.Parallel()

		// This is the failure the rewrite can have that a separate renderer
		// could not: a fragment that spells an argument reference in a way the
		// pattern does not match keeps that spelling, and the statement reaches
		// the server with a literal sqlc.arg(created_after) in it. Loud when it
		// happens, but only at execution, and only for whichever fragment moved.
		for _, d := range everyDialect() {
			for name, b := range boundStatements(t, d) {
				test.StrNotContains(t, b.SQL, "sqlc.", test.Sprintf("dialect %q, statement %q", d, name))
			}
		}
	})

	T.Run("agree with themselves about their arguments", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			for name, b := range boundStatements(t, d) {
				assertMarkersMatchArgs(t, d, b)
				test.SliceNotEmpty(t, b.Args, test.Sprintf("dialect %q, statement %q", d, name))
			}
		}
	})

	T.Run("key every single-row statement on the scope as well as the id", func(t *testing.T) {
		t.Parallel()

		// The read path that omits the scope is the one a caller reaches for
		// without having thought about tenancy, so there is deliberately no
		// statement here that has an id predicate and no scope predicate.
		for _, d := range everyDialect() {
			statements := boundStatements(t, d)

			for _, name := range []string{"get", "exists", "update", "archive"} {
				test.SliceContains(t, statements[name].Args, BelongsToAccountColumn,
					test.Sprintf("dialect %q, statement %q", d, name))
				test.SliceContains(t, statements[name].Args, IDColumn,
					test.Sprintf("dialect %q, statement %q", d, name))
			}

			// And the one that addresses a set of rows has no id predicate at
			// all, which is what makes it a page rather than a read.
			test.SliceNotContains(t, statements["list"].Args, IDColumn, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("exists asks what get asks", func(t *testing.T) {
		t.Parallel()

		// Not the same statement — one reads the row and one reports it — but
		// the same predicates, so a caller cannot be told a row exists and then
		// be refused it.
		for _, d := range everyDialect() {
			statements := boundStatements(t, d)

			test.Eq(t, statements["get"].Args, statements["exists"].Args, test.Sprintf("dialect %q", d))
		}
	})
}

func TestBound_Bind(T *testing.T) {
	T.Parallel()

	T.Run("assembles the values in placeholder order", func(t *testing.T) {
		t.Parallel()

		b := Bound{Args: []string{"second", "first", "second"}}

		got, err := b.Bind(map[string]any{"first": 1, "second": 2, "unused": 3})

		must.NoError(t, err)
		test.Eq(t, []any{2, 1, 2}, got)
	})

	T.Run("binds a repeated name once per occurrence", func(t *testing.T) {
		t.Parallel()

		// Which is what makes the positional dialects work: the statement has a
		// marker per occurrence and the driver is handed a value per marker.
		g := For(dialect.MySQL)
		b := g.BoundList(boundTable, boundColumns())

		values := map[string]any{}
		g.BindFilter(values, nil)

		got, err := b.Bind(values)

		must.NoError(t, err)
		test.SliceLen(t, len(b.Args), got)
		test.Greater(t, len(values), len(got))
	})

	T.Run("refuses a name it was given no value for", func(t *testing.T) {
		t.Parallel()

		// A nil is a legitimate value for every nullable argument here, so an
		// absent key cannot be read as one: the two are indistinguishable once
		// bound, and the statement would filter on a NULL nobody chose.
		got, err := Bound{Args: []string{CursorArg}}.Bind(map[string]any{})

		must.Error(t, err)
		test.ErrorIs(t, err, ErrUnboundArgument)
		test.StrContains(t, err.Error(), CursorArg)
		test.Nil(t, got)
	})

	T.Run("accepts an explicit nil", func(t *testing.T) {
		t.Parallel()

		got, err := Bound{Args: []string{CursorArg}}.Bind(map[string]any{CursorArg: nil})

		must.NoError(t, err)
		test.Eq(t, []any{nil}, got)
	})

	T.Run("a statement with no arguments binds to an empty slice", func(t *testing.T) {
		t.Parallel()

		got, err := Bound{}.Bind(nil)

		must.NoError(t, err)
		test.SliceEmpty(t, got)
	})
}

func TestBindFilter(T *testing.T) {
	T.Parallel()

	T.Run("writes every argument the emitted filter binds", func(t *testing.T) {
		t.Parallel()

		// The mapping between filtering's field names and these argument names
		// is what this package emits, so a caller assembling the map itself
		// would be a second copy of it — and a copy that spelled created_after
		// "createdAfter" would bind nothing and filter nothing, which looks
		// exactly like a filter nobody set.
		at := time.Now().UTC()

		values := map[string]any{}
		For(dialect.Postgres).BindFilter(values, &filtering.QueryFilter{
			CreatedAfter:    &at,
			CreatedBefore:   &at,
			UpdatedAfter:    &at,
			UpdatedBefore:   &at,
			IncludeArchived: pointer.To(true),
			Cursor:          pointer.To("w_001"),
			MaxResponseSize: pointer.To(uint16(25)),
		})

		test.EqOp(t, any(database.NullTimeFromTimePointer(&at)), values[CreatedAfterArg])
		test.EqOp(t, any(database.NullTimeFromTimePointer(&at)), values[CreatedBeforeArg])
		test.EqOp(t, any(database.NullTimeFromTimePointer(&at)), values[UpdatedAfterArg])
		test.EqOp(t, any(database.NullTimeFromTimePointer(&at)), values[UpdatedBeforeArg])
		test.EqOp(t, any(database.NullBoolFromBoolPointer(pointer.To(true))), values[IncludeArchivedArg])
		test.EqOp(t, any(database.NullStringFromStringPointer(pointer.To("w_001"))), values[CursorArg])
		test.EqOp(t, any(database.NullInt32FromUint16Pointer(pointer.To(uint16(25)))), values[LimitArg])
	})

	T.Run("hands SQLite a window it can compare", func(t *testing.T) {
		t.Parallel()

		// SQLite stores these columns as the text CURRENT_TIMESTAMP wrote and
		// compares them lexicographically. A time bound as a time reaches it as
		// a number, and its affinity rules put every number below every string,
		// so the comparison is true for every row — a window that filters
		// nothing and says nothing.
		at := time.Date(2026, time.August, 20, 17, 54, 42, 0, time.UTC)

		values := map[string]any{}
		For(dialect.SQLite).BindFilter(values, &filtering.QueryFilter{CreatedAfter: &at})

		test.EqOp(t, any("2026-08-20 17:54:42"), values[CreatedAfterArg])

		// And in the same shape the DDL's default writes, which is the schema
		// requirement the package comment states.
		test.EqOp(t, "2026-08-20 17:54:42", at.Format(time.DateTime))
	})

	T.Run("hands SQLite a NULL for a bound nobody set", func(t *testing.T) {
		t.Parallel()

		// Rather than the zero time formatted, which is a string the emitted
		// COALESCE would prefer to its sentinel and which excludes nothing only
		// by accident.
		values := map[string]any{}
		For(dialect.SQLite).BindFilter(values, &filtering.QueryFilter{})

		test.Nil(t, values[CreatedAfterArg])
		test.Nil(t, values[UpdatedBeforeArg])
	})

	T.Run("binds MySQL the page size the other two coalesce to", func(t *testing.T) {
		t.Parallel()

		// MySQL's LIMIT takes a placeholder and nothing else, so there is no
		// COALESCE in its emitted SQL to supply a default and a NULL there is an
		// empty page — which is what a filter matching nothing looks like too.
		// The other two coalesce in the SQL, so a NULL is the right bind.
		values := map[string]any{}
		For(dialect.MySQL).BindFilter(values, &filtering.QueryFilter{})

		test.EqOp(t, any(int32(filtering.DefaultQueryFilterLimit)), values[LimitArg])

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			values = map[string]any{}
			For(d).BindFilter(values, &filtering.QueryFilter{})

			test.EqOp(t, any(database.NullInt32FromUint16Pointer(nil)), values[LimitArg],
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("leaves a page size the caller set to zero alone", func(t *testing.T) {
		t.Parallel()

		// An explicit zero means no rows on every dialect, which is loud. Only
		// absence is defaulted, the same distinction Normalize draws.
		for _, d := range everyDialect() {
			values := map[string]any{}
			For(d).BindFilter(values, &filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(0))})

			test.EqOp(t, any(database.NullInt32FromUint16Pointer(pointer.To(uint16(0)))), values[LimitArg],
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("a nil filter binds the defaults rather than nothing", func(t *testing.T) {
		t.Parallel()

		// A caller that took no filter still has to produce a bindable
		// statement: every one of these arguments has a placeholder whatever
		// the caller asked for.
		values := map[string]any{}
		For(dialect.Postgres).BindFilter(values, nil)

		defaults := filtering.DefaultQueryFilter()

		test.EqOp(t, any(database.NullInt32FromUint16Pointer(defaults.MaxResponseSize)), values[LimitArg])
		test.EqOp(t, any(database.NullBoolFromBoolPointer(defaults.IncludeArchived)), values[IncludeArchivedArg])
	})

	T.Run("leaves what the caller already put in the map alone", func(t *testing.T) {
		t.Parallel()

		// The match columns are bound by the same map, and a filter that
		// cleared them would be a keyed read that lost its key.
		values := map[string]any{BelongsToAccountColumn: "account_one"}
		For(dialect.Postgres).BindFilter(values, nil)

		test.EqOp(t, any("account_one"), values[BelongsToAccountColumn])
	})

	T.Run("is enough to bind a list statement on every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			b := For(d).BoundList(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn})

			values := map[string]any{BelongsToAccountColumn: "account_one"}
			For(d).BindFilter(values, filtering.DefaultQueryFilter())

			got, err := b.Bind(values)

			must.NoError(t, err, must.Sprintf("dialect %q", d))
			test.SliceLen(t, len(b.Args), got, test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_boundIsPerStatement(T *testing.T) {
	T.Parallel()

	T.Run("rendering a bound statement does not change what the Generator emits for sqlc", func(t *testing.T) {
		t.Parallel()

		// Structural now — bindArguments takes a statement and returns a new
		// one, and the Generator holds nothing a rewrite could leave behind.
		// Asserted anyway because the consequence of losing it is a generator
		// binary writing $1 into the .sql files it emits, which is SQL that
		// generates nothing and reads like sqlc broke.
		for _, d := range everyDialect() {
			g := For(d)

			before := g.getStatement(boundTable, boundColumns(), "", Read{})
			_ = g.BoundGet(boundTable, boundColumns())
			_ = g.BoundList(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn})
			after := g.getStatement(boundTable, boundColumns(), "", Read{})

			test.EqOp(t, before, after, test.Sprintf("dialect %q", d))
			test.StrContains(t, after, "sqlc.arg("+IDColumn+")", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("each statement numbers its own arguments from one", func(t *testing.T) {
		t.Parallel()

		// The ordinals bindArguments hands out are positions in one
		// statement's argument list, so a Generator held for the lifetime of a
		// store and asked for many statements cannot carry them between calls.
		g := For(dialect.Postgres)

		first := g.BoundGet(boundTable, boundColumns())
		second := g.BoundGet(boundTable, boundColumns())

		test.EqOp(t, first.SQL, second.SQL)
		test.StrContains(t, second.SQL, "$1")
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
			got := For(d).BoundList(boundTable, boundColumns())

			names := slices.Sorted(slices.Values(got.Args))
			names = slices.Compact(names)

			test.Eq(t, expected, names, test.Sprintf("dialect %q", d))
		}
	})
}

// The fixture for a table whose primary key is natural rather than a surrogate
// id — shredding_subject_keys' shape, whose key is what enforces one live key
// per subject. It has every column this package has an opinion about except the
// one it used to insist on.
const compositeTable = "sprockets"

func compositeColumns() []string {
	return []string{
		"subject_type",
		"subject_id",
		"key_material",
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

// compositeKey is what such a store passes where a conventional one passes
// nothing and lets the id predicate appear on its own.
func compositeKey() []Match {
	return []Match{{Column: "subject_type"}, {Column: "subject_id"}}
}

// compositeStatements is the composite-key counterpart of boundStatements: the
// four single-row statements, each keyed on the whole natural key.
func compositeStatements(d dialect.Dialect) map[string]Bound {
	var (
		g       = For(d)
		columns = compositeColumns()
		key     = compositeKey()
	)

	return map[string]Bound{
		"get":     g.BoundGet(compositeTable, columns, key...),
		"exists":  g.BoundExists(compositeTable, columns, key...),
		"update":  g.BoundUpdate(compositeTable, columns, ForUpdate(columns, "subject_type", "subject_id"), nil, key...),
		"archive": g.BoundArchive(compositeTable, columns, key...),
	}
}

func TestBoundStatementsWithoutAnID(T *testing.T) {
	T.Parallel()

	T.Run("address a row by the whole natural key", func(t *testing.T) {
		t.Parallel()

		// The id predicate is presence-conditional like every other predicate
		// here, so a table that deliberately has no id gets a statement keyed
		// on what it does key on rather than one naming a column the schema
		// does not have.
		for _, d := range everyDialect() {
			for name, b := range compositeStatements(d) {
				test.SliceContains(t, b.Args, "subject_type", test.Sprintf("dialect %q, statement %q", d, name))
				test.SliceContains(t, b.Args, "subject_id", test.Sprintf("dialect %q, statement %q", d, name))
				test.SliceNotContains(t, b.Args, IDColumn, test.Sprintf("dialect %q, statement %q", d, name))
				test.StrNotContains(t, b.SQL, Qualify(compositeTable, IDColumn),
					test.Sprintf("dialect %q, statement %q", d, name))
				test.StrNotContains(t, b.SQL, "sqlc.", test.Sprintf("dialect %q, statement %q", d, name))
				assertMarkersMatchArgs(t, d, b)
			}
		}
	})

	T.Run("still exclude archived rows outright", func(t *testing.T) {
		t.Parallel()

		// Losing this alongside the id predicate would be the quiet half of the
		// change: a get that reads shredded rows back, and an archive that
		// restamps one already archived and reports it as a write.
		for _, d := range everyDialect() {
			statements := compositeStatements(d)

			test.StrContains(t, statements["get"].SQL, Qualify(compositeTable, ArchivedAtColumn)+" IS NULL",
				test.Sprintf("dialect %q", d))
			test.StrContains(t, statements["archive"].SQL, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
			test.StrContains(t, statements["update"].SQL, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("exists selects something every server accepts rather than a column the table lacks", func(t *testing.T) {
		t.Parallel()

		// Nothing reads the EXISTS subquery's projection, so the id case keeps
		// selecting the id — the emitted SQL of every conventional table in the
		// module is unchanged — and the no-id case selects the literal.
		for _, d := range everyDialect() {
			test.StrContains(t, compositeStatements(d)["exists"].SQL, "SELECT 1", test.Sprintf("dialect %q", d))
			test.StrContains(t, For(d).BoundExists(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn}).SQL,
				"SELECT "+Qualify(boundTable, IDColumn), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("exists asks what get asks", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			statements := compositeStatements(d)

			test.Eq(t, statements["get"].Args, statements["exists"].Args, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("archive keys the row on exactly what the other three key it on", func(t *testing.T) {
		t.Parallel()

		// archiveStatement used to build its own predicate list, which is how
		// it came to be the one statement here that could not skip the id. It
		// routes through singleRowPredicates now, so there is one rendering of
		// "this row, unarchived" rather than two.
		for _, d := range everyDialect() {
			statements := compositeStatements(d)

			test.Eq(t, statements["get"].Args, statements["archive"].Args, test.Sprintf("dialect %q", d))
		}

		// And on a conventional table it still keys on the id, in the position
		// it always did.
		conventional := For(dialect.Postgres).BoundArchive(boundTable, boundColumns(),
			Match{Column: BelongsToAccountColumn})

		test.Eq(t, []string{IDColumn, BelongsToAccountColumn}, conventional.Args)
	})

	T.Run("the archived predicate follows the column list like the id one", func(t *testing.T) {
		t.Parallel()

		// The parameter BoundArchive gained is the table's columns, so a caller
		// handing it a subset gets a statement derived from that subset. It is
		// the reason the doc says to pass the table's columns rather than the
		// ones this call happens to touch.
		for _, d := range everyDialect() {
			got := For(d).BoundArchive(compositeTable, without(compositeColumns(), ArchivedAtColumn), compositeKey()...)

			test.StrNotContains(t, got.SQL, "IS NULL", test.Sprintf("dialect %q", d))
		}
	})
}

func TestBoundStatementsKeyOnSomething(T *testing.T) {
	T.Parallel()

	T.Run("a single-row statement with no id and no match is refused", func(t *testing.T) {
		t.Parallel()

		// Rather than a WHERE clause that is the archived predicate and nothing
		// else: a get returning the whole table, an update assigning every row
		// in it, and an archive emptying it.
		columns := without(compositeColumns(), IDColumn)

		for _, d := range everyDialect() {
			g := For(d)

			statements := map[string]func(){
				"get":     func() { _ = g.BoundGet(compositeTable, columns) },
				"exists":  func() { _ = g.BoundExists(compositeTable, columns) },
				"update":  func() { _ = g.BoundUpdate(compositeTable, columns, ForUpdate(columns), nil) },
				"archive": func() { _ = g.BoundArchive(compositeTable, columns) },
			}

			for name, render := range statements {
				err := recovered(render)

				must.Error(t, err, must.Sprintf("dialect %q, statement %q", d, name))
				test.ErrorIs(t, err, ErrUnaddressableRow, test.Sprintf("dialect %q, statement %q", d, name))
				test.StrContains(t, err.Error(), compositeTable, test.Sprintf("dialect %q, statement %q", d, name))
			}
		}
	})

	T.Run("one match is enough, and so is an id on its own", func(t *testing.T) {
		t.Parallel()

		// The requirement is that the statement keys on something, not that it
		// keys on the whole primary key — this package never reads a schema and
		// has no way to know what the whole key is.
		for _, d := range everyDialect() {
			columns := without(compositeColumns(), IDColumn)

			test.SliceNotEmpty(t, For(d).BoundGet(compositeTable, columns, Match{Column: "subject_id"}).Args,
				test.Sprintf("dialect %q", d))
			test.SliceNotEmpty(t, For(d).BoundGet(boundTable, boundColumns()).Args, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("the list is unaffected, because a page is not a row", func(t *testing.T) {
		t.Parallel()

		// An unkeyed list is the ordinary case: it addresses a page of a table
		// rather than a row of one, and the filter and the cursor are what
		// bound it.
		for _, d := range everyDialect() {
			err := recovered(func() { _ = For(d).BoundList(compositeTable, without(compositeColumns(), IDColumn)) })

			must.NoError(t, err, must.Sprintf("dialect %q", d))
		}
	})
}
