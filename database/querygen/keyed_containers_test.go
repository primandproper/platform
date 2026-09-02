package querygen

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database/dialect"
	"github.com/primandproper/platform-go/v14/filtering"
	"github.com/primandproper/platform-go/v14/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// keyed_query_test.go checks that a keyed variant is the standard statement with
// more predicates. That is an assertion about text, and text is only half of it:
// the reason this package renders a statement at all is that a server executes
// it, and the standard suite beside this one executes only the statements a
// conventional table gets.
//
// The variants are what a store's corpus adds to that set — a get keyed on an
// owner, a read that projects one column and excludes rather than matches, an
// update guarded by the value it is replacing, a table whose primary key is
// natural. Each of those is SQL a server either accepts or does not, and the
// failures are not all loud: a predicate dropped from a count is a page whose
// totals belong to another tenant, and an exclusion rendered as an equality
// answers the row it was asked to avoid.
//
// So the variants get the same treatment the standard set gets: run, against one
// of each server, asserting the behavior rather than the SQL. This suite hangs
// off runDialect beside the standard one and stands up its own tables, so
// neither can see the other's writes.

const (
	gadgetOwner = "account_one"
	gadgetOther = "account_two"
)

// execQuery binds the values a statement names and runs it, failing on either.
//
// It is what a generated querier does with a params struct, spelled for a
// harness that holds its values in a map: the statement is rendered once, up
// front, and bound per execution.
func execQuery(tb testing.TB, ctx context.Context, db *sql.DB, d dialect.Dialect, q *Query, values map[string]any) sql.Result {
	tb.Helper()

	statement, order := bindQuery(d, q)

	result, err := db.ExecContext(ctx, statement, argumentsFor(tb, order, values)...)
	must.NoError(tb, err, must.Sprintf("executing\n%s", statement))

	return result
}

func affectedRows(tb testing.TB, result sql.Result) int64 {
	tb.Helper()

	affected, err := result.RowsAffected()
	must.NoError(tb, err)

	return affected
}

// text reads a column back as a string, whichever shape the driver handed it
// over in — MySQL hands a VARCHAR back as bytes.
func text(tb testing.TB, read any) string {
	tb.Helper()

	if got, ok := read.(string); ok {
		return got
	}

	raw, isBytes := read.([]byte)
	must.True(tb, isBytes, must.Sprintf("column came back as %T", read))

	return string(raw)
}

func insertGadget(tb testing.TB, ctx context.Context, db *sql.DB, d dialect.Dialect, queries map[string]*Query, id, name, account string) {
	tb.Helper()

	execQuery(tb, ctx, db, d, queries["create"], map[string]any{
		IDColumn:               id,
		"name":                 name,
		BelongsToAccountColumn: account,
	})
}

// getGadget runs the keyed get, returning the name it read and whether there was
// a row at all.
func getGadget(tb testing.TB, ctx context.Context, db *sql.DB, d dialect.Dialect, queries map[string]*Query, id, account string) (name string, found bool) {
	tb.Helper()

	statement, order := bindQuery(d, queries["get"])
	arguments := argumentsFor(tb, order, map[string]any{IDColumn: id, BelongsToAccountColumn: account})

	var (
		gotID                                     string
		gotAccount                                string
		indexed, created, updated, archived, read any
	)

	err := db.QueryRowContext(ctx, statement, arguments...).
		Scan(&gotID, &read, &gotAccount, &indexed, &created, &updated, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}

	must.NoError(tb, err)

	return text(tb, read), true
}

// listGadgets runs the keyed list for one account, returning the page and the
// two counts beside it.
func listGadgets(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, queries map[string]*Query, account string, filter *filtering.QueryFilter) (ids []string, filtered, total int64) {
	tb.Helper()

	values := map[string]any{BelongsToAccountColumn: account}
	filterValues(d, values, filter)

	statement, order := bindQuery(d, queries["list"])

	rows, err := db.QueryContext(ctx, statement, argumentsFor(tb, order, values)...)
	must.NoError(tb, err, must.Sprintf("executing\n%s", statement))

	defer func() { must.NoError(tb, rows.Close()) }()

	for rows.Next() {
		var (
			id                                               string
			name, owner, indexed, created, updated, archived any
			rowFiltered, rowTotal                            int64
		)

		must.NoError(tb, rows.Scan(&id, &name, &owner, &indexed, &created, &updated, &archived, &rowFiltered, &rowTotal))

		ids = append(ids, id)
		filtered, total = rowFiltered, rowTotal
	}

	must.NoError(tb, rows.Err())

	return ids, filtered, total
}

// assertServerAccepts prepares every statement, which is the cheapest assertion
// a server can make about SQL it has not been given values for.
func assertServerAccepts(t *testing.T, ctx context.Context, db *sql.DB, d dialect.Dialect, queries map[string]*Query) {
	t.Helper()

	for name := range queries {
		statement, _ := bindQuery(d, queries[name])

		stmt, prepareErr := db.PrepareContext(ctx, statement)
		must.NoError(t, prepareErr, must.Sprintf("preparing %s:\n%s", name, statement))
		must.NoError(t, stmt.Close()) //nolint:sqlclosecheck // the prepare is the assertion; there is nothing to read.
	}
}

// runKeyedSuite is the keyed counterpart of runWidgetSuite, and like it is
// written once and run against each of the three servers.
func runKeyedSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, conventionalDDL(d, keyedTable))
	must.NoError(t, err)

	queries := keyedQueries(d)

	t.Run("every keyed statement is one the server accepts", func(t *testing.T) {
		assertServerAccepts(t, ctx, db, d, queries)
	})

	for _, id := range []string{"g_003", "g_001", "g_004", "g_002"} {
		insertGadget(t, ctx, db, d, queries, id, "gadget "+id, gadgetOwner)
	}

	insertGadget(t, ctx, db, d, queries, "g_005", "someone else's gadget", gadgetOther)

	t.Run("a read is answered within its scope and refused outside it", func(t *testing.T) {
		name, found := getGadget(t, ctx, db, d, queries, "g_001", gadgetOwner)

		test.True(t, found)
		test.EqOp(t, "gadget g_001", name)

		// The row exists; the scope is what withholds it. A statement whose
		// extra predicate went missing would return it here, and the caller
		// would never learn it had read across a tenant boundary.
		_, found = getGadget(t, ctx, db, d, queries, "g_005", gadgetOwner)
		test.False(t, found)
	})

	t.Run("exists reports what get would find", func(t *testing.T) {
		statement, order := bindQuery(d, queries["exists"])

		cases := []struct {
			id, account string
			want        bool
		}{
			{"g_001", gadgetOwner, true},
			{"g_005", gadgetOwner, false},
			{"g_005", gadgetOther, true},
			{"g_nope", gadgetOwner, false},
		}

		for i := range cases {
			tc := cases[i]

			arguments := argumentsFor(t, order, map[string]any{
				IDColumn:               tc.id,
				BelongsToAccountColumn: tc.account,
			})

			var got bool

			must.NoError(t, db.QueryRowContext(ctx, statement, arguments...).Scan(&got))
			test.EqOp(t, tc.want, got, test.Sprintf("%s in %s", tc.id, tc.account))
		}
	})

	t.Run("a keyed read projects one column, excludes, and picks in a named order", func(t *testing.T) {
		// The three things ReadQuery adds over GetQuery, in one statement,
		// because each of them is SQL a server either accepts or does not: a
		// projection narrower than the table, a predicate that excludes rather
		// than matches, and the ORDER BY ... LIMIT 1 that makes "another row
		// like this one" a row rather than whichever the planner reached first.
		//
		// It runs before anything archives, so the four live gadgets are
		// g_001 through g_004 and the answer is not a matter of timing.
		columns := keyedColumns()

		statement, order := bindQuery(d, For(d).ReadQuery("GetAnotherGadgetID", keyedTable, without(columns, IDColumn),
			Read{Projection: []string{IDColumn}, Order: IDColumn},
			Match{Column: BelongsToAccountColumn},
			Match{Column: "name", Exclude: true}))

		arguments := argumentsFor(t, order, map[string]any{
			BelongsToAccountColumn: gadgetOwner,
			"name":                 "gadget g_001",
		})

		var got string

		must.NoError(t, db.QueryRowContext(ctx, statement, arguments...).Scan(&got),
			must.Sprintf("executing\n%s", statement))

		// g_001 is excluded by name and g_005 belongs to someone else, so the
		// first row in id order is g_002. A statement whose exclusion had
		// rendered as an equality would answer g_001, and one that had lost its
		// ordering could answer any of the three.
		test.EqOp(t, "g_002", got)

		// The same read with nothing left to find is no rows rather than an
		// arbitrary one — which is what lets a caller branch on sql.ErrNoRows.
		arguments = argumentsFor(t, order, map[string]any{
			BelongsToAccountColumn: "account_nobody",
			"name":                 "gadget g_001",
		})

		test.ErrorIs(t, db.QueryRowContext(ctx, statement, arguments...).Scan(&got), sql.ErrNoRows)
	})

	t.Run("the list pages without its counts moving", func(t *testing.T) {
		// filtered_count carries the window and the archived toggle but not the
		// cursor, so it answers "how many are left" rather than "how many are
		// on this page" — and it is the same answer on the fiftieth page as on
		// the first.
		first, filtered, total := listGadgets(t, ctx, d, db, queries, gadgetOwner,
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2))})

		test.Eq(t, []string{"g_001", "g_002"}, first)
		test.EqOp(t, int64(4), filtered)
		test.EqOp(t, int64(4), total)

		second, filtered, total := listGadgets(t, ctx, d, db, queries, gadgetOwner,
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2)), Cursor: pointer.To("g_002")})

		test.Eq(t, []string{"g_003", "g_004"}, second)
		test.EqOp(t, int64(4), filtered)
		test.EqOp(t, int64(4), total)
	})

	t.Run("the list counts its own scope and not the table", func(t *testing.T) {
		// The other account's row is in the table and in neither count: a keyed
		// list whose counts were unkeyed would report five here, which reads as
		// a pagination bug somewhere else entirely.
		ids, filtered, total := listGadgets(t, ctx, d, db, queries, gadgetOther, nil)

		test.Eq(t, []string{"g_005"}, ids)
		test.EqOp(t, int64(1), filtered)
		test.EqOp(t, int64(1), total)
	})

	t.Run("a bound window filters", func(t *testing.T) {
		// This is the assertion SQLite needs. Its timestamps are text and its
		// comparisons over them lexicographic, and a time handed to the driver
		// as a time arrives as a number — which its affinity rules sort below
		// every string, so the window admits every row for every bound. The
		// page would look correct, the count would look correct, and the filter
		// would be doing nothing. See filterTime.
		future := time.Now().Add(time.Hour)

		ids, filtered, _ := listGadgets(t, ctx, d, db, queries, gadgetOwner,
			&filtering.QueryFilter{CreatedAfter: &future})

		test.SliceEmpty(t, ids)
		test.EqOp(t, int64(0), filtered)

		past := time.Now().Add(-time.Hour)

		ids, filtered, _ = listGadgets(t, ctx, d, db, queries, gadgetOwner,
			&filtering.QueryFilter{CreatedAfter: &past})

		test.SliceLen(t, 4, ids)
		test.EqOp(t, int64(4), filtered)
	})

	t.Run("an update writes within its scope and no further", func(t *testing.T) {
		values := map[string]any{
			IDColumn:               "g_001",
			"name":                 "renamed",
			BelongsToAccountColumn: gadgetOwner,
		}

		test.EqOp(t, int64(1), affectedRows(t, execQuery(t, ctx, db, d, queries["update"], values)))

		name, found := getGadget(t, ctx, db, d, queries, "g_001", gadgetOwner)
		test.True(t, found)
		test.EqOp(t, "renamed", name)

		// The same statement, aimed at another account's row: no error and no
		// row, which is the only report a caller gets and the reason the update
		// counts rows rather than execing.
		values[IDColumn] = "g_005"

		test.EqOp(t, int64(0), affectedRows(t, execQuery(t, ctx, db, d, queries["update"], values)))

		name, found = getGadget(t, ctx, db, d, queries, "g_005", gadgetOther)
		test.True(t, found)
		test.EqOp(t, "someone else's gadget", name)
	})

	t.Run("archive soft-deletes once and reports the second attempt", func(t *testing.T) {
		values := map[string]any{IDColumn: "g_004", BelongsToAccountColumn: gadgetOwner}

		test.EqOp(t, int64(1), affectedRows(t, execQuery(t, ctx, db, d, queries["archive"], values)))

		// The archived_at IS NULL in the WHERE is what makes the second count
		// zero rather than one.
		test.EqOp(t, int64(0), affectedRows(t, execQuery(t, ctx, db, d, queries["archive"], values)))

		_, found := getGadget(t, ctx, db, d, queries, "g_004", gadgetOwner)
		test.False(t, found)
	})

	t.Run("include_archived admits the archived row rather than decorating the query", func(t *testing.T) {
		ids, filtered, _ := listGadgets(t, ctx, d, db, queries, gadgetOwner, nil)
		test.Eq(t, []string{"g_001", "g_002", "g_003"}, ids)
		test.EqOp(t, int64(3), filtered)

		ids, filtered, _ = listGadgets(t, ctx, d, db, queries, gadgetOwner,
			&filtering.QueryFilter{IncludeArchived: pointer.To(true)})

		test.Eq(t, []string{"g_001", "g_002", "g_003", "g_004"}, ids)
		test.EqOp(t, int64(4), filtered)
	})
}

// The natural-key half of the suite. Its table has no id at all, so the four
// single-row statements key on the whole natural key, and the thing worth
// running against a server is that both of its columns bind: a statement that
// dropped one would still parse, still return a row, and return the wrong one
// whenever two rows share the column it kept.
//
// It stands its own table up beside gadgets for the same reason gadgets stands
// one up beside widgets — neither suite should be able to see the other's writes.

// compositeDDL is compositeColumns() in each dialect's spelling, with the
// natural key as the primary key. The dialect differences are conventionalDDL's:
// MySQL cannot key on a TEXT column without a prefix length, and SQLite's
// timestamps are the text its own CURRENT_TIMESTAMP writes.
func compositeDDL(d dialect.Dialect) string {
	switch d {
	case dialect.MySQL:
		return `CREATE TABLE ` + compositeTable + ` (
			subject_type VARCHAR(64) NOT NULL,
			subject_id VARCHAR(64) NOT NULL,
			key_material VARCHAR(255) NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_updated_at DATETIME NULL,
			archived_at DATETIME NULL,
			PRIMARY KEY (subject_type, subject_id)
		)`
	case dialect.SQLite:
		return `CREATE TABLE ` + compositeTable + ` (
			subject_type TEXT NOT NULL,
			subject_id TEXT NOT NULL,
			key_material TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_updated_at TEXT,
			archived_at TEXT,
			PRIMARY KEY (subject_type, subject_id)
		)`
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return `CREATE TABLE ` + compositeTable + ` (
			subject_type TEXT NOT NULL,
			subject_id TEXT NOT NULL,
			key_material TEXT NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			last_updated_at TIMESTAMP WITH TIME ZONE,
			archived_at TIMESTAMP WITH TIME ZONE,
			PRIMARY KEY (subject_type, subject_id)
		)`
	}
}

// subjectKey is the argument map a natural-keyed statement binds its key from,
// which is the whole difference at the call site: two entries where a
// conventional store writes one.
func subjectKey(subjectType, subjectID string) map[string]any {
	return map[string]any{"subject_type": subjectType, "subject_id": subjectID}
}

// getSubjectKey runs the keyed get, returning the key material it read and
// whether there was a row at all.
func getSubjectKey(tb testing.TB, ctx context.Context, db *sql.DB, d dialect.Dialect, queries map[string]*Query, subjectType, subjectID string) (material string, found bool) {
	tb.Helper()

	statement, order := bindQuery(d, queries["get"])
	arguments := argumentsFor(tb, order, subjectKey(subjectType, subjectID))

	var (
		gotType, gotID             string
		read                       any
		created, updated, archived any
	)

	err := db.QueryRowContext(ctx, statement, arguments...).
		Scan(&gotType, &gotID, &read, &created, &updated, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}

	must.NoError(tb, err)

	return text(tb, read), true
}

// runCompositeSuite is the natural-key counterpart of runKeyedSuite, and like it
// is written once and run against each of the three servers.
func runCompositeSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, compositeDDL(d))
	must.NoError(t, err)

	queries := compositeQueries(d)

	t.Run("every natural-key statement is one the server accepts", func(t *testing.T) {
		assertServerAccepts(t, ctx, db, d, queries)
	})

	// Three rows across two subject types, deliberately sharing an id between
	// two of them: the row a half-keyed statement would return instead.
	rows := []struct{ subjectType, subjectID, material string }{
		{"user", "s_001", "user s_001 material"},
		{"device", "s_001", "device s_001 material"},
		{"user", "s_002", "user s_002 material"},
	}

	for i := range rows {
		values := subjectKey(rows[i].subjectType, rows[i].subjectID)
		values["key_material"] = rows[i].material

		execQuery(t, ctx, db, d, queries["create"], values)
	}

	t.Run("a read is answered by the whole key rather than half of it", func(t *testing.T) {
		// Two rows share s_001 and two share user, so a statement that dropped
		// either column of the key would answer here — with a row, and with the
		// wrong one. That is the failure the id predicate's unconditional
		// rendering used to make unrepresentable by making the table
		// unrepresentable.
		material, found := getSubjectKey(t, ctx, db, d, queries, "user", "s_001")

		test.True(t, found)
		test.EqOp(t, "user s_001 material", material)

		material, found = getSubjectKey(t, ctx, db, d, queries, "device", "s_001")

		test.True(t, found)
		test.EqOp(t, "device s_001 material", material)

		// A pairing no row has, whose halves are each present.
		_, found = getSubjectKey(t, ctx, db, d, queries, "device", "s_002")
		test.False(t, found)
	})

	t.Run("exists reports what get would find", func(t *testing.T) {
		statement, order := bindQuery(d, queries["exists"])

		cases := []struct {
			subjectType, subjectID string
			want                   bool
		}{
			{"user", "s_001", true},
			{"device", "s_001", true},
			{"device", "s_002", false},
			{"nobody", "s_001", false},
		}

		for i := range cases {
			tc := cases[i]

			arguments := argumentsFor(t, order, subjectKey(tc.subjectType, tc.subjectID))

			var got bool

			must.NoError(t, db.QueryRowContext(ctx, statement, arguments...).Scan(&got))
			test.EqOp(t, tc.want, got, test.Sprintf("%s/%s", tc.subjectType, tc.subjectID))
		}
	})

	t.Run("an update writes the row its key names and no other", func(t *testing.T) {
		values := subjectKey("user", "s_001")
		values["key_material"] = "rotated"

		test.EqOp(t, int64(1), affectedRows(t, execQuery(t, ctx, db, d, queries["update"], values)))

		material, found := getSubjectKey(t, ctx, db, d, queries, "user", "s_001")
		test.True(t, found)
		test.EqOp(t, "rotated", material)

		// The two rows sharing a column with it are untouched, which is the
		// assertion an update keyed on one half of the key would fail.
		material, found = getSubjectKey(t, ctx, db, d, queries, "device", "s_001")
		test.True(t, found)
		test.EqOp(t, "device s_001 material", material)

		material, found = getSubjectKey(t, ctx, db, d, queries, "user", "s_002")
		test.True(t, found)
		test.EqOp(t, "user s_002 material", material)
	})

	t.Run("archive soft-deletes once and reports the second attempt", func(t *testing.T) {
		values := subjectKey("user", "s_002")

		test.EqOp(t, int64(1), affectedRows(t, execQuery(t, ctx, db, d, queries["archive"], values)))

		// The archived_at IS NULL that makes the second count zero survived
		// archiveStatement's move onto singleRowPredicates.
		test.EqOp(t, int64(0), affectedRows(t, execQuery(t, ctx, db, d, queries["archive"], values)))

		_, found := getSubjectKey(t, ctx, db, d, queries, "user", "s_002")
		test.False(t, found)

		// And the row it shares a subject type with is still live.
		_, found = getSubjectKey(t, ctx, db, d, queries, "user", "s_001")
		test.True(t, found)
	})

	t.Run("an archived row is not updatable", func(t *testing.T) {
		values := subjectKey("user", "s_002")
		values["key_material"] = "should not land"

		test.EqOp(t, int64(0), affectedRows(t, execQuery(t, ctx, db, d, queries["update"], values)))
	})
}
