package querygen

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/dialect"
	"github.com/primandproper/platform-go/v12/filtering"
	"github.com/primandproper/platform-go/v12/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The claim this file exists to check is that there is no second rendering: the
// bound statements are the emitted ones with a different argument spelling, and
// nothing else about them moved. So the oracle for every builder below is the
// sqlc statement it corresponds to, put through the same rewrite sqlc performs
// before a driver sees a query — which containers_test.go already owns as bind,
// because running the emitted text rather than a paraphrase of it is what that
// suite is for.
//
// An assertion of the form "the bound SQL equals bind(sqlc SQL)" fails for two
// different reasons, and both are the ones worth hearing about: a bound path
// that renders its own predicate, and a bound path that numbers its placeholders
// differently from the way sqlc numbers them.

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

// assertBindsSQLC checks that got is what sqlc's own rewrite makes of the
// statement the generator emits for the same table.
func assertBindsSQLC(tb testing.TB, d dialect.Dialect, got Bound, sqlcStatement string) {
	tb.Helper()

	wantSQL, wantArgs := bind(d, sqlcStatement)

	test.EqOp(tb, wantSQL, got.SQL, test.Sprintf("dialect %q", d))
	test.Eq(tb, wantArgs, got.Args, test.Sprintf("dialect %q", d))
}

// assertPlaceholdersMatchArgs checks the invariant a driver enforces at
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
func assertPlaceholdersMatchArgs(tb testing.TB, d dialect.Dialect, b Bound) {
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

	T.Run("is the emitted get with driver placeholders", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			assertBindsSQLC(t, d, g.BoundGet(boundTable, boundColumns()),
				g.getStatement(boundTable, boundColumns(), ""))
		}
	})

	T.Run("keys on the extra match columns as well as the id", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got := For(d).BoundGet(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn})

			test.Eq(t, []string{IDColumn, BelongsToAccountColumn}, got.Args, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, Qualify(boundTable, BelongsToAccountColumn)+" =", test.Sprintf("dialect %q", d))
			assertPlaceholdersMatchArgs(t, d, got)
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

func TestGenerator_BoundExists(T *testing.T) {
	T.Parallel()

	T.Run("is the emitted exists with driver placeholders", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			assertBindsSQLC(t, d, g.BoundExists(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn}),
				g.existsStatement(boundTable, boundColumns(), "", Match{Column: BelongsToAccountColumn}))
		}
	})
}

func TestGenerator_BoundCreate(T *testing.T) {
	T.Parallel()

	T.Run("is the emitted insert with driver placeholders", func(t *testing.T) {
		t.Parallel()

		insert := ForInsert(boundColumns())

		for _, d := range everyDialect() {
			g := For(d)
			got := g.BoundCreate(boundTable, insert, nil)

			assertBindsSQLC(t, d, got, g.createStatement(boundTable, insert, nil))

			// The insert's arguments are its column list, in order, which is
			// what lets a caller assemble the map from the same slice.
			test.Eq(t, insert, got.Args, test.Sprintf("dialect %q", d))
		}
	})

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

	T.Run("is the emitted update with driver placeholders", func(t *testing.T) {
		t.Parallel()

		var (
			columns  = boundColumns()
			updates  = ForUpdate(columns, BelongsToAccountColumn)
			nullable = []string{LastIndexedAtColumn}
			match    = Match{Column: BelongsToAccountColumn}
		)

		for _, d := range everyDialect() {
			g := For(d)

			assertBindsSQLC(t, d, g.BoundUpdate(boundTable, columns, updates, nullable, match),
				g.updateStatement(boundTable, columns, updates, "", nullable, match))
		}
	})

	T.Run("assigns before it keys, so the argument order is the assignments then the predicates", func(t *testing.T) {
		t.Parallel()

		updates := ForUpdate(boundColumns(), BelongsToAccountColumn)

		for _, d := range everyDialect() {
			got := For(d).BoundUpdate(boundTable, boundColumns(), updates, nil, Match{Column: BelongsToAccountColumn})

			test.Eq(t, append(slices.Clone(updates), IDColumn, BelongsToAccountColumn), got.Args,
				test.Sprintf("dialect %q", d))
			assertPlaceholdersMatchArgs(t, d, got)
		}
	})

	T.Run("a column that is both assigned and matched is one argument", func(t *testing.T) {
		t.Parallel()

		// Which is a statement that sets a column to the value it is being
		// required to already hold — legal, useless, and the sqlc path's
		// behavior too, since WithOwnership renders the owner into the SET and
		// the WHERE from the same argument name. It is named here so that the
		// argument list stops being a surprise: a caller wanting to move a row
		// between owners wants the owner column out of its updatable set, which
		// is what ForUpdate's exceptions are for.
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
}

func TestGenerator_BoundArchive(T *testing.T) {
	T.Parallel()

	T.Run("is the emitted archive with driver placeholders", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			assertBindsSQLC(t, d, g.BoundArchive(boundTable, Match{Column: BelongsToAccountColumn}),
				g.archiveStatement(boundTable, "", Match{Column: BelongsToAccountColumn}))
		}
	})

	T.Run("stamps rather than deletes, and only an unarchived row", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got := For(d).BoundArchive(boundTable)

			test.StrContains(t, got.SQL, "UPDATE "+boundTable, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, ArchivedAtColumn+" = "+NowExpression, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, got.SQL, "DELETE", test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_BoundList(T *testing.T) {
	T.Parallel()

	T.Run("is the emitted list with driver placeholders", func(t *testing.T) {
		t.Parallel()

		// A single match is what WithOwnership renders on the sqlc side, so the
		// two are the same statement and can be compared directly.
		for _, d := range everyDialect() {
			g := For(d)

			assertBindsSQLC(t, d, g.BoundList(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn}),
				g.listStatement(boundTable, boundColumns(), BelongsToAccountColumn))
		}
	})

	T.Run("without matches, is the unkeyed list", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			g := For(d)

			assertBindsSQLC(t, d, g.BoundList(boundTable, boundColumns()),
				g.listStatement(boundTable, boundColumns(), ""))
		}
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
			got := For(d).BoundList(boundTable, boundColumns())

			assertPlaceholdersMatchArgs(t, d, got)

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
			assertPlaceholdersMatchArgs(t, d, got)
		}
	})
}

func TestGenerator_BoundArchiveMatching(T *testing.T) {
	T.Parallel()

	T.Run("archives every unarchived row the matches select", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			got, err := For(d).BoundArchiveMatching(boundTable, Match{Column: BelongsToAccountColumn})

			must.NoError(t, err, must.Sprintf("dialect %q", d))
			test.Eq(t, []string{BelongsToAccountColumn}, got.Args, test.Sprintf("dialect %q", d))
			test.StrContains(t, got.SQL, ArchivedAtColumn+" IS NULL", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, got.SQL, IDColumn+" = ", test.Sprintf("dialect %q", d))
			assertPlaceholdersMatchArgs(t, d, got)
		}
	})

	T.Run("keys its WHERE without a table qualifier", func(t *testing.T) {
		t.Parallel()

		// An UPDATE's SET cannot carry one, and a WHERE that does while the SET
		// does not is a parse error on MySQL rather than a style difference.
		for _, d := range everyDialect() {
			got, err := For(d).BoundArchiveMatching(boundTable, Match{Column: BelongsToAccountColumn})

			must.NoError(t, err, must.Sprintf("dialect %q", d))
			test.StrNotContains(t, got.SQL, Qualify(boundTable, BelongsToAccountColumn), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses the empty match set", func(t *testing.T) {
		t.Parallel()

		// With no matches the only predicate left is archived_at IS NULL, which
		// is every live row in the table.
		for _, d := range everyDialect() {
			got, err := For(d).BoundArchiveMatching(boundTable)

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, ErrUnboundableStatement, test.Sprintf("dialect %q", d))
			test.EqOp(t, "", got.SQL, test.Sprintf("dialect %q", d))
		}
	})
}

func TestGenerator_BoundIDSet(T *testing.T) {
	T.Parallel()

	T.Run("binds the whole set as one array on Postgres", func(t *testing.T) {
		t.Parallel()

		// ANY over a bound array rather than `=`, which compares a text column
		// against a text[] and is an error rather than an empty result.
		predicate, args := For(dialect.Postgres).BoundIDSet(boundTable, 3)

		test.EqOp(t, Qualify(boundTable, IDColumn)+" = ANY($1::text[])", predicate)
		test.Eq(t, []string{IDsArg}, args)
	})

	T.Run("renders the same text whatever the count, on Postgres", func(t *testing.T) {
		t.Parallel()

		first, _ := For(dialect.Postgres).BoundIDSet(boundTable, 1)
		many, _ := For(dialect.Postgres).BoundIDSet(boundTable, 40)

		test.EqOp(t, first, many)
	})

	T.Run("expands to one placeholder per id on the dialects without an array type", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			predicate, args := For(d).BoundIDSet(boundTable, 3)

			test.EqOp(t, Qualify(boundTable, IDColumn)+" IN (?, ?, ?)", predicate, test.Sprintf("dialect %q", d))
			test.Eq(t, []string{"ids#0", "ids#1", "ids#2"}, args, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("names each expanded element unmistakably", func(t *testing.T) {
		t.Parallel()

		// No identifier dialect.ValidIdentifier accepts contains a '#', so an
		// element cannot collide with a column bound in the same statement.
		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			_, args := For(d).BoundIDSet(boundTable, 2)

			for _, name := range args {
				test.False(t, dialect.ValidIdentifier(name), test.Sprintf("dialect %q, argument %q", d, name))
			}
		}
	})

	T.Run("agrees with itself about how many arguments it wants", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			for _, count := range []int{0, 1, 7} {
				predicate, args := For(d).BoundIDSet(boundTable, count)

				want := len(args)
				if d == dialect.Postgres {
					want = 1
				}

				test.EqOp(t, want, len(placeholder.FindAllString(predicate, -1)),
					test.Sprintf("dialect %q, count %d", d, count))
			}
		}
	})
}

func TestBoundBinder_slice(T *testing.T) {
	T.Parallel()

	T.Run("refuses a statement whose arity is not known until the values are", func(t *testing.T) {
		t.Parallel()

		// idSetPredicate is the only caller, and it takes the array arm on
		// Postgres. Reaching here means a new caller arrived on a dialect that
		// expands a set, and it needs BoundIDSet rather than this.
		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			scoped := &Generator{bind: boundBinder{dialect: d}, dialect: d}

			err := recovered(func() { _ = scoped.idSetPredicate() })

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, ErrUnboundableStatement, test.Sprintf("dialect %q", d))
			test.StrContains(t, err.Error(), IDsArg, test.Sprintf("dialect %q", d))
		}
	})
}

func TestBoundBinder_resolve(T *testing.T) {
	T.Parallel()

	T.Run("refuses a statement whose sentinels do not pair up", func(t *testing.T) {
		t.Parallel()

		// Sentinels are written in pairs, so an odd one means an identifier
		// carried a NUL past dialect.ValidIdentifier. Reading on would take the
		// SQL either side of it for an argument name and shift every argument
		// after it — a statement that binds a limit where a cursor belongs, and
		// runs.
		for _, d := range everyDialect() {
			b := boundBinder{dialect: d}

			err := recovered(func() { _, _ = b.resolve("SELECT " + boundSentinel + IDColumn) })

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, ErrUnboundableStatement, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("leaves a statement with no arguments alone", func(t *testing.T) {
		t.Parallel()

		sql, args := boundBinder{dialect: dialect.Postgres}.resolve("SELECT 1;")

		test.EqOp(t, "SELECT 1;", sql)
		test.SliceEmpty(t, args)
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
		test.EqOp(t, "2026-08-20 17:54:42", at.Format(SQLiteTimestampLayout))
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

func TestGenerator_bindingIsPerStatement(T *testing.T) {
	T.Parallel()

	T.Run("a Bound call does not change what the Generator emits for sqlc", func(t *testing.T) {
		t.Parallel()

		// The positional binder is installed on a copy for the duration of one
		// statement. If it leaked onto the Generator, a generator binary that
		// rendered one bound statement would emit $1 into the .sql files it
		// wrote afterwards — SQL that generates nothing and looks like sqlc
		// broke.
		for _, d := range everyDialect() {
			g := For(d)

			before := g.getStatement(boundTable, boundColumns(), "")
			_ = g.BoundGet(boundTable, boundColumns())
			_ = g.BoundList(boundTable, boundColumns(), Match{Column: BelongsToAccountColumn})
			after := g.getStatement(boundTable, boundColumns(), "")

			test.EqOp(t, before, after, test.Sprintf("dialect %q", d))
			test.StrContains(t, after, "sqlc.arg("+IDColumn+")", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("each statement numbers its own arguments from one", func(t *testing.T) {
		t.Parallel()

		// The ordinals a binder hands out are positions in one statement's
		// argument list, so a Generator held for the lifetime of a store cannot
		// share one.
		g := For(dialect.Postgres)

		first := g.BoundGet(boundTable, boundColumns())
		second := g.BoundGet(boundTable, boundColumns())

		test.EqOp(t, first.SQL, second.SQL)
		test.StrContains(t, second.SQL, "$1")
	})
}
