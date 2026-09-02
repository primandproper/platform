package querygen

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database"
	"github.com/primandproper/platform-go/v14/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/filtering"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// This package emits text, and the only thing that decides whether the text is
// right is a server executing it. That is what the container suites do, and to
// execute a canonical statement they first have to do what sqlc's generated code
// does before it hands one to a driver: rewrite the argument references into
// bind markers, and hand over the values in the order the markers take them.
//
// So the rewrite lives here, in the test harness, rather than in the package. It
// is not a second consumer path — nothing outside these tests renders a
// statement for a driver, and the generated queriers sqlc-gen-unison emits are
// what a store executes. It is the fixture that lets a test assert about
// behavior instead of about strings.
//
// It is small, and it is still worth checking: a binder that miscounted markers
// would not fail the suites, it would bind every value after the first into the
// wrong slot and let them assert confidently about the wrong rows. Hence
// TestBindArguments below, and assertMarkersMatchArgs beside it.

// errUnbindableStatement indicates a statement the harness has no executable
// rendering for, because the number of placeholders it needs is not known until
// the values are.
var errUnbindableStatement = platformerrors.New("statement cannot be rendered as bound SQL")

// sqlcArgument matches an argument reference in emitted SQL: the required and
// nullable forms, the set form that has no single rendering, and the bare `?`
// that is MySQL's page size.
//
// The name alphabet admits '#' because sqlc's own expansion of a set synthesizes
// one name per element, and a synthetic name has to be unmistakable for a real
// one — see expandSlices. No identifier dialect.ValidIdentifier accepts contains
// a '#', so the two cannot collide.
//
// The bare marker is here because one argument cannot be named in the SQL that
// declares it: MySQL's grammar takes a placeholder after LIMIT and nothing else,
// so Generator.limitClause writes the marker itself. It is unambiguous — no
// other fragment this package emits contains a '?', and LIMIT is the clause a
// statement ends on, so the marker is matched where it appears like every other
// and lands last in the argument order.
var sqlcArgument = regexp.MustCompile(`sqlc\.(n?arg|slice)\(([a-zA-Z0-9_#]+)\)|\?`)

// bindArguments rewrites a statement's sqlc argument references into d's bind
// markers, returning the statement a driver takes and the argument each marker
// stands for, in the order the driver takes them.
//
// It is a pass over the finished statement rather than a decision made while
// rendering one, and that is load-bearing. A fragment here is rendered once and
// spliced more than once: filterPredicates goes into the SELECT's WHERE and into
// both count subqueries, so created_after appears three times in a list query
// from one rendering of it. Numbering at render time would give that one
// rendering one marker and record one argument while the statement carried
// three. Numbering the finished text has no such problem: a marker is numbered
// where it appears, however it got there.
//
// What differs per dialect is only whether a repeat is one argument or several.
// Postgres numbers its markers, so repeats reuse the first occurrence's number
// and the value is bound once. MySQL's and SQLite's are positional — a bare `?`
// takes the next value and there is no way to name an earlier one — so there
// each occurrence appends the name again and the caller binds the value as many
// times as it appears. Both are correct; only the count differs.
//
// SQLite's own syntax does have a numbered form (`?NNN`), but dialect.Placeholder
// does not emit it, and treating SQLite as numbered here would collapse a
// repeat's arguments while leaving its markers in place — a statement that binds
// every value after the first into the following slot rather than one that fails
// to parse.
//
// A set reference has no rendering at all: sqlc.slice is a macro sqlc expands
// per call, because the number of markers a set needs is not known until the
// values are. A caller that wants one expands it first — see expandSlices —
// so reaching one here is the harness being handed something it cannot execute.
func bindArguments(d dialect.Dialect, statement string) (sql string, args []string) {
	ordinals := map[string]int{}

	sql = sqlcArgument.ReplaceAllStringFunc(statement, func(reference string) string {
		parts := sqlcArgument.FindStringSubmatch(reference)
		kind, name := parts[1], parts[2]

		// The bare marker, which only MySQL's limit clause emits — it stands
		// for the page size and is numbered here like a named reference. On
		// any other dialect a '?' in a statement this package rendered is a
		// marker somebody placed by hand, and the name it stands for is not
		// recoverable, so it says so rather than binding the page size to it.
		if kind == "" {
			if d != dialect.MySQL {
				panic(platformerrors.Wrapf(errUnbindableStatement, "querygen: %s statement contains an unnamed bind marker", d))
			}

			name = LimitArg
		}

		if kind == "slice" {
			panic(platformerrors.Wrapf(errUnbindableStatement, "querygen: argument %q is a set", name))
		}

		if n, ok := ordinals[name]; ok && d == dialect.Postgres {
			return d.Placeholder(n)
		}

		args = append(args, name)
		ordinals[name] = len(args)

		return d.Placeholder(len(args))
	})

	return sql, args
}

// bindQuery is bindArguments over a canonical Query: the statement a driver
// takes, and the names its markers stand for.
func bindQuery(d dialect.Dialect, q *Query) (sql string, args []string) {
	return bindArguments(d, q.Content)
}

// filterValues writes a filtering.QueryFilter's values into an argument map
// under the names the emitted statements bind them by.
//
// It is the harness's stand-in for what a generated querier does with a params
// struct, and it carries the two things that are not simply "the field's value",
// both of which are dialect facts rather than test conveniences:
//
// A time is not one value on all three servers — see filterTime. Everything
// else is, and is written through database's null helpers so that an unset field
// reaches the server as the NULL the emitted COALESCE expects rather than as a
// zero.
//
// A nil filter binds the defaults, so a read that took no filter still produces
// a bindable statement. It writes only the arguments it owns: a keyed read's
// match columns go into the same map, and a filter that cleared them would be a
// read that lost its key.
func filterValues(d dialect.Dialect, values map[string]any, filter *filtering.QueryFilter) {
	if filter == nil {
		filter = filtering.DefaultQueryFilter()
	}

	values[CreatedAfterArg] = filterTime(d, filter.CreatedAfter)
	values[CreatedBeforeArg] = filterTime(d, filter.CreatedBefore)
	values[UpdatedAfterArg] = filterTime(d, filter.UpdatedAfter)
	values[UpdatedBeforeArg] = filterTime(d, filter.UpdatedBefore)
	values[IncludeArchivedArg] = database.NullBoolFromBoolPointer(filter.IncludeArchived)
	values[CursorArg] = database.NullStringFromStringPointer(filter.Cursor)
	values[LimitArg] = filterLimit(d, filter.MaxResponseSize)
}

// filterLimit renders the page size bound.
//
// Postgres and SQLite take an expression after LIMIT, so the emitted SQL
// coalesces an absent size to filtering.DefaultQueryFilterLimit and a NULL binds
// correctly. MySQL takes a placeholder and nothing else — COALESCE there is a
// parse error rather than a slower plan — so its LIMIT binds whatever it is
// handed, and what a NULL gets is an empty page. Binding the constant the other
// two coalesce to is what keeps that from being a dialect a reader of the suite
// has to remember; filtering.QueryFilter.ToSQLArgs answers absence the same way,
// from the same constant, for the callers that are not this harness.
//
// A size the caller set to zero is left alone, and returns no rows on every
// dialect. That is the documented meaning of an explicit zero — loud, rather
// than a page of some other size — and only absence is defaulted here.
func filterLimit(d dialect.Dialect, size *uint16) any {
	if size != nil || d != dialect.MySQL {
		return database.NullInt32FromUint16Pointer(size)
	}

	return int32(filtering.DefaultQueryFilterLimit)
}

// filterTime renders a window bound as the value this dialect's comparisons
// expect.
//
// Postgres and MySQL have a timestamp type and drivers that speak time.Time, so
// a NullTime is what they take. SQLite has neither: its columns hold the text
// CURRENT_TIMESTAMP wrote and its comparisons over one are lexicographic, so a
// time.Time arrives as whatever the driver made of it — and under SQLite's type
// affinity rules a number sorts below every string, so a text column compared
// against one is greater than it whatever the two of them mean.
//
// time.DateTime is that shape: it is the layout SQLite's own CURRENT_TIMESTAMP
// writes, which is the schema requirement the package comment states.
//
// The failure this prevents is the quiet kind: a created_after bound as a time
// on SQLite admits every row in the table, for every value of the bound. No
// error, no empty page, just a window that does nothing — which is
// indistinguishable from a caller who set no window at all. A generated querier
// owes its SQLite callers the same shape; see identity's package comment for
// where the current driver gets it right by accident.
func filterTime(d dialect.Dialect, at *time.Time) any {
	if d != dialect.SQLite {
		return database.NullTimeFromTimePointer(at)
	}

	if at == nil {
		return nil
	}

	return at.UTC().Format(time.DateTime)
}

// placeholder matches either dialect family's bind marker. Neither character
// appears in the emitted SQL outside one, so counting matches counts
// placeholders.
var placeholder = regexp.MustCompile(`\$\d+|\?`)

// assertMarkersMatchArgs checks the invariant a driver enforces at execution
// time: every marker in the statement has a value, and every value has a marker.
//
// The two schemes satisfy it differently, which is the reason it is worth
// asserting at all rather than counting markers. A positional marker consumes
// the next value, so the count of them is the count of arguments. A numbered one
// names its value, so a repeat reuses an ordinal and there are more markers than
// arguments — what has to hold there is that the ordinals are exactly 1..len,
// with none skipped: an ordinal past the end is a driver error, and a gap is an
// argument nothing reads while everything after it is off by one.
func assertMarkersMatchArgs(tb testing.TB, d dialect.Dialect, sql string, args []string) {
	tb.Helper()

	markers := placeholder.FindAllString(sql, -1)

	if d != dialect.Postgres {
		test.EqOp(tb, len(markers), len(args),
			test.Sprintf("dialect %q: markers and arguments disagree in\n%s", d, sql))

		return
	}

	seen := make(map[int]bool, len(args))

	for _, marker := range markers {
		n, err := strconv.Atoi(strings.TrimPrefix(marker, "$"))
		must.NoError(tb, err, must.Sprintf("dialect %q: marker %q", d, marker))
		seen[n] = true
	}

	test.MapLen(tb, len(args), seen, test.Sprintf("dialect %q: in\n%s", d, sql))

	for n := 1; n <= len(args); n++ {
		test.True(tb, seen[n], test.Sprintf("dialect %q: no $%d in\n%s", d, n, sql))
	}
}

func TestBindArguments(T *testing.T) {
	T.Parallel()

	T.Run("refuses a set, whose arity is not known until the values are", func(t *testing.T) {
		t.Parallel()

		// sqlc.slice is a macro sqlc expands per call, because the arity
		// belongs to the values. A statement carrying one has no single
		// executable rendering, and the harness expands it first rather than
		// guessing — see expandSlices. setPredicate is the only fragment that
		// renders one, and only off Postgres, and the two statements built on
		// it — the bulk stamp and the batched read — are corpus-only for
		// exactly this reason.
		for _, d := range everyDialect() {
			err := recovered(func() { _, _ = bindArguments(d, For(dialect.MySQL).setPredicate(IDColumn, IDsArg)) })

			must.Error(t, err, must.Sprintf("dialect %q", d))
			test.ErrorIs(t, err, errUnbindableStatement, test.Sprintf("dialect %q", d))
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
			test.ErrorIs(t, err, errUnbindableStatement, test.Sprintf("dialect %q", d))
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

func TestFilterValues(T *testing.T) {
	T.Parallel()

	T.Run("hands SQLite a window it can compare", func(t *testing.T) {
		t.Parallel()

		// SQLite stores these columns as the text CURRENT_TIMESTAMP wrote and
		// compares them lexicographically. A time bound as a time reaches it as
		// a number, and its affinity rules put every number below every string,
		// so the comparison is true for every row — a window that filters
		// nothing and says nothing.
		at := time.Date(2026, time.August, 20, 17, 54, 42, 0, time.UTC)

		values := map[string]any{}
		filterValues(dialect.SQLite, values, &filtering.QueryFilter{CreatedAfter: &at})

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
		filterValues(dialect.SQLite, values, &filtering.QueryFilter{})

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
		filterValues(dialect.MySQL, values, &filtering.QueryFilter{})

		test.EqOp(t, any(int32(filtering.DefaultQueryFilterLimit)), values[LimitArg])

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			values = map[string]any{}
			filterValues(d, values, &filtering.QueryFilter{})

			test.EqOp(t, any(database.NullInt32FromUint16Pointer(nil)), values[LimitArg],
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("leaves what the caller already put in the map alone", func(t *testing.T) {
		t.Parallel()

		// The match columns are bound by the same map, and a filter that
		// cleared them would be a keyed read that lost its key.
		values := map[string]any{BelongsToAccountColumn: "account_one"}
		filterValues(dialect.Postgres, values, nil)

		test.EqOp(t, any("account_one"), values[BelongsToAccountColumn])
	})

	T.Run("is enough to bind a list statement on every dialect", func(t *testing.T) {
		t.Parallel()

		for _, d := range everyDialect() {
			_, args := bindQuery(d, pagedList(For(d).ListQueries("ListGadgets", keyedTable, keyedColumns(),
				Match{Column: BelongsToAccountColumn}), Ascending))

			values := map[string]any{BelongsToAccountColumn: "account_one"}
			filterValues(d, values, filtering.DefaultQueryFilter())

			for _, name := range args {
				_, ok := values[name]
				test.True(t, ok, test.Sprintf("dialect %q: no value for %q", d, name))
			}
		}
	})
}
