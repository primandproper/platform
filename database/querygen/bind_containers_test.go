package querygen

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/filtering"
	"github.com/primandproper/platform-go/v13/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// bind_test.go checks that the bound statements are the emitted ones with a
// different argument spelling, against the rewrite sqlc performs. That is an
// assertion about text, and text is only half of it: the reason this package
// renders a statement at all is that a server executes it, and the bound path
// has a failure mode the sqlc path does not.
//
// A statement rendered here is handed to a driver directly rather than to a
// generator, so the count and order of its arguments are this package's problem
// rather than sqlc's. Getting them wrong does not always fail: a marker too many
// is an error a driver raises, but a value in the wrong slot is a query that
// runs and answers about something else — a cursor compared against a limit, a
// window compared against an account id.
//
// So the bound statements get the same treatment the emitted ones get: run,
// against one of each server, asserting the behavior rather than the SQL. This
// suite hangs off runDialect beside the sqlc one and stands up its own table, so
// neither can see the other's writes.

const gadgetsTable = "gadgets"

const (
	gadgetOwner = "account_one"
	gadgetOther = "account_two"
)

// gadgetStatements is what a scoped store would hold: one statement per
// operation, each keyed on the owner as well as on whatever it already keyed on.
//
// Rendering them once up front is the usage this package is built for — a store
// renders at construction and executes per request — and it is also the thing
// worth checking, since a Bound that only works when rendered per call would
// have hidden the repeated-argument bug rather than exposed it.
type gadgetStatements struct {
	create  Bound
	get     Bound
	exists  Bound
	update  Bound
	archive Bound
	list    Bound
}

func gadgetsFor(d dialect.Dialect) *gadgetStatements {
	var (
		g       = For(d)
		columns = widgetsColumns()
		owner   = Match{Column: BelongsToAccountColumn}
	)

	return &gadgetStatements{
		create: g.BoundCreate(gadgetsTable, ForInsert(columns), nil),
		get:    g.BoundGet(gadgetsTable, columns, owner),
		exists: g.BoundExists(gadgetsTable, columns, owner),
		// The owner is out of the updatable set: a statement that assigns the
		// column it keys on binds one argument to both, and there is no value
		// of it that moves a row anywhere.
		update:  g.BoundUpdate(gadgetsTable, columns, ForUpdate(columns, BelongsToAccountColumn), nil, owner),
		archive: g.BoundArchive(gadgetsTable, columns, owner),
		list:    g.BoundList(gadgetsTable, columns, owner),
	}
}

func (s *gadgetStatements) all() map[string]Bound {
	return map[string]Bound{
		"create":  s.create,
		"get":     s.get,
		"exists":  s.exists,
		"update":  s.update,
		"archive": s.archive,
		"list":    s.list,
	}
}

// execBound binds the values and runs the statement, failing on either.
func execBound(tb testing.TB, ctx context.Context, db *sql.DB, b Bound, values map[string]any) sql.Result {
	tb.Helper()

	arguments, err := b.Bind(values)
	must.NoError(tb, err)

	result, err := db.ExecContext(ctx, b.SQL, arguments...)
	must.NoError(tb, err, must.Sprintf("executing\n%s", b.SQL))

	return result
}

func affectedRows(tb testing.TB, result sql.Result) int64 {
	tb.Helper()

	affected, err := result.RowsAffected()
	must.NoError(tb, err)

	return affected
}

func insertGadget(tb testing.TB, ctx context.Context, db *sql.DB, s *gadgetStatements, id, name, account string) {
	tb.Helper()

	execBound(tb, ctx, db, s.create, map[string]any{
		IDColumn:               id,
		"name":                 name,
		BelongsToAccountColumn: account,
	})
}

// getGadget runs the bound get, returning the name it read and whether there was
// a row at all.
func getGadget(tb testing.TB, ctx context.Context, db *sql.DB, s *gadgetStatements, id, account string) (name string, found bool) {
	tb.Helper()

	arguments, err := s.get.Bind(map[string]any{IDColumn: id, BelongsToAccountColumn: account})
	must.NoError(tb, err)

	var (
		gotID                                     string
		gotAccount                                string
		indexed, created, updated, archived, read any
	)

	err = db.QueryRowContext(ctx, s.get.SQL, arguments...).
		Scan(&gotID, &read, &gotAccount, &indexed, &created, &updated, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}

	must.NoError(tb, err)

	got, ok := read.(string)
	if !ok {
		// MySQL hands a VARCHAR back as bytes.
		raw, isBytes := read.([]byte)
		must.True(tb, isBytes, must.Sprintf("name came back as %T", read))
		got = string(raw)
	}

	return got, true
}

// listGadgets runs the bound list for one account, returning the page and the
// two counts beside it.
func listGadgets(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, s *gadgetStatements, account string, filter *filtering.QueryFilter) (ids []string, filtered, total int64) {
	tb.Helper()

	values := map[string]any{BelongsToAccountColumn: account}
	For(d).BindFilter(values, filter)

	arguments, err := s.list.Bind(values)
	must.NoError(tb, err)

	rows, err := db.QueryContext(ctx, s.list.SQL, arguments...)
	must.NoError(tb, err, must.Sprintf("executing\n%s", s.list.SQL))

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

// runBoundSuite is the bound counterpart of runWidgetSuite, and like it is
// written once and run against each of the three servers.
func runBoundSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, conventionalDDL(d, gadgetsTable))
	must.NoError(t, err)

	statements := gadgetsFor(d)

	t.Run("every bound statement is one the server accepts", func(t *testing.T) {
		// Unlike the emitted statements, these need no rewriting first: what a
		// Bound holds is what a driver is handed.
		all := statements.all()
		for name := range all {
			stmt, prepareErr := db.PrepareContext(ctx, all[name].SQL)
			must.NoError(t, prepareErr, must.Sprintf("preparing %s:\n%s", name, all[name].SQL))
			must.NoError(t, stmt.Close()) //nolint:sqlclosecheck // the prepare is the assertion; there is nothing to read.
		}
	})

	for _, id := range []string{"g_003", "g_001", "g_004", "g_002"} {
		insertGadget(t, ctx, db, statements, id, "gadget "+id, gadgetOwner)
	}

	insertGadget(t, ctx, db, statements, "g_005", "someone else's gadget", gadgetOther)

	t.Run("a read is answered within its scope and refused outside it", func(t *testing.T) {
		name, found := getGadget(t, ctx, db, statements, "g_001", gadgetOwner)

		test.True(t, found)
		test.EqOp(t, "gadget g_001", name)

		// The row exists; the scope is what withholds it. A bound statement
		// whose extra predicate went missing would return it here, and the
		// caller would never learn it had read across a tenant boundary.
		_, found = getGadget(t, ctx, db, statements, "g_005", gadgetOwner)
		test.False(t, found)
	})

	t.Run("exists reports what get would find", func(t *testing.T) {
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

			arguments, bindErr := statements.exists.Bind(map[string]any{
				IDColumn:               tc.id,
				BelongsToAccountColumn: tc.account,
			})
			must.NoError(t, bindErr)

			var got bool

			must.NoError(t, db.QueryRowContext(ctx, statements.exists.SQL, arguments...).Scan(&got))
			test.EqOp(t, tc.want, got, test.Sprintf("%s in %s", tc.id, tc.account))
		}
	})

	t.Run("the list pages without its counts moving", func(t *testing.T) {
		// filtered_count carries the window and the archived toggle but not the
		// cursor, so it answers "how many are left" rather than "how many are
		// on this page" — and it is the same answer on the fiftieth page as on
		// the first.
		first, filtered, total := listGadgets(t, ctx, d, db, statements, gadgetOwner,
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2))})

		test.Eq(t, []string{"g_001", "g_002"}, first)
		test.EqOp(t, int64(4), filtered)
		test.EqOp(t, int64(4), total)

		second, filtered, total := listGadgets(t, ctx, d, db, statements, gadgetOwner,
			&filtering.QueryFilter{MaxResponseSize: pointer.To(uint16(2)), Cursor: pointer.To("g_002")})

		test.Eq(t, []string{"g_003", "g_004"}, second)
		test.EqOp(t, int64(4), filtered)
		test.EqOp(t, int64(4), total)
	})

	t.Run("the list counts its own scope and not the table", func(t *testing.T) {
		// The other account's row is in the table and in neither count: a keyed
		// list whose counts were unkeyed would report five here, which reads as
		// a pagination bug somewhere else entirely.
		ids, filtered, total := listGadgets(t, ctx, d, db, statements, gadgetOther, nil)

		test.Eq(t, []string{"g_005"}, ids)
		test.EqOp(t, int64(1), filtered)
		test.EqOp(t, int64(1), total)
	})

	t.Run("a window bound through BindFilter filters", func(t *testing.T) {
		// This is the assertion SQLite needs. Its timestamps are text and its
		// comparisons over them lexicographic, and a time handed to the driver
		// as a time arrives as a number — which its affinity rules sort below
		// every string, so the window admits every row for every bound. The
		// page would look correct, the count would look correct, and the filter
		// would be doing nothing.
		future := time.Now().Add(time.Hour)

		ids, filtered, _ := listGadgets(t, ctx, d, db, statements, gadgetOwner,
			&filtering.QueryFilter{CreatedAfter: &future})

		test.SliceEmpty(t, ids)
		test.EqOp(t, int64(0), filtered)

		past := time.Now().Add(-time.Hour)

		ids, filtered, _ = listGadgets(t, ctx, d, db, statements, gadgetOwner,
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

		test.EqOp(t, int64(1), affectedRows(t, execBound(t, ctx, db, statements.update, values)))

		name, found := getGadget(t, ctx, db, statements, "g_001", gadgetOwner)
		test.True(t, found)
		test.EqOp(t, "renamed", name)

		// The same statement, aimed at another account's row: no error and no
		// row, which is the only report a caller gets and the reason the update
		// counts rows rather than execing.
		values[IDColumn] = "g_005"

		test.EqOp(t, int64(0), affectedRows(t, execBound(t, ctx, db, statements.update, values)))

		name, found = getGadget(t, ctx, db, statements, "g_005", gadgetOther)
		test.True(t, found)
		test.EqOp(t, "someone else's gadget", name)
	})

	t.Run("archive soft-deletes once and reports the second attempt", func(t *testing.T) {
		values := map[string]any{IDColumn: "g_004", BelongsToAccountColumn: gadgetOwner}

		test.EqOp(t, int64(1), affectedRows(t, execBound(t, ctx, db, statements.archive, values)))

		// The archived_at IS NULL in the WHERE is what makes the second count
		// zero rather than one.
		test.EqOp(t, int64(0), affectedRows(t, execBound(t, ctx, db, statements.archive, values)))

		_, found := getGadget(t, ctx, db, statements, "g_004", gadgetOwner)
		test.False(t, found)
	})

	t.Run("include_archived admits the archived row rather than decorating the query", func(t *testing.T) {
		ids, filtered, _ := listGadgets(t, ctx, d, db, statements, gadgetOwner, nil)
		test.Eq(t, []string{"g_001", "g_002", "g_003"}, ids)
		test.EqOp(t, int64(3), filtered)

		ids, filtered, _ = listGadgets(t, ctx, d, db, statements, gadgetOwner,
			&filtering.QueryFilter{IncludeArchived: pointer.To(true)})

		test.Eq(t, []string{"g_001", "g_002", "g_003", "g_004"}, ids)
		test.EqOp(t, int64(4), filtered)
	})
}

// The composite-key half of the bound suite. Its table has no id at all, so the
// four single-row statements key on the whole natural key, and the thing worth
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

// subjectKey is the argument map a composite-keyed statement binds its key from,
// which is the whole difference at the call site: two entries where a
// conventional store writes one.
func subjectKey(subjectType, subjectID string) map[string]any {
	return map[string]any{"subject_type": subjectType, "subject_id": subjectID}
}

// getSubjectKey runs the bound get, returning the key material it read and
// whether there was a row at all.
func getSubjectKey(tb testing.TB, ctx context.Context, db *sql.DB, statements map[string]Bound, subjectType, subjectID string) (material string, found bool) {
	tb.Helper()

	arguments, err := statements["get"].Bind(subjectKey(subjectType, subjectID))
	must.NoError(tb, err)

	var (
		gotType, gotID             string
		read                       any
		created, updated, archived any
	)

	err = db.QueryRowContext(ctx, statements["get"].SQL, arguments...).
		Scan(&gotType, &gotID, &read, &created, &updated, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}

	must.NoError(tb, err)

	got, ok := read.(string)
	if !ok {
		// MySQL hands a VARCHAR back as bytes.
		raw, isBytes := read.([]byte)
		must.True(tb, isBytes, must.Sprintf("key material came back as %T", read))
		got = string(raw)
	}

	return got, true
}

// runCompositeSuite is the composite-key counterpart of runBoundSuite, and like
// it is written once and run against each of the three servers.
func runCompositeSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, compositeDDL(d))
	must.NoError(t, err)

	var (
		g          = For(d)
		columns    = compositeColumns()
		statements = compositeStatements(d)
	)

	// The create is not one of the four this ticket is about — an INSERT keys on
	// nothing — but the suite needs rows.
	create := g.BoundCreate(compositeTable, ForInsert(columns), nil)

	t.Run("every bound statement is one the server accepts", func(t *testing.T) {
		all := map[string]Bound{"create": create}
		for name := range statements {
			all[name] = statements[name]
		}

		for name := range all {
			stmt, prepareErr := db.PrepareContext(ctx, all[name].SQL)
			must.NoError(t, prepareErr, must.Sprintf("preparing %s:\n%s", name, all[name].SQL))
			must.NoError(t, stmt.Close()) //nolint:sqlclosecheck // the prepare is the assertion; there is nothing to read.
		}
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

		execBound(t, ctx, db, create, values)
	}

	t.Run("a read is answered by the whole key rather than half of it", func(t *testing.T) {
		// Two rows share s_001 and two share user, so a statement that dropped
		// either column of the key would answer here — with a row, and with the
		// wrong one. That is the failure the id predicate's unconditional
		// rendering used to make unrepresentable by making the table
		// unrepresentable.
		material, found := getSubjectKey(t, ctx, db, statements, "user", "s_001")

		test.True(t, found)
		test.EqOp(t, "user s_001 material", material)

		material, found = getSubjectKey(t, ctx, db, statements, "device", "s_001")

		test.True(t, found)
		test.EqOp(t, "device s_001 material", material)

		// A pairing no row has, whose halves are each present.
		_, found = getSubjectKey(t, ctx, db, statements, "device", "s_002")
		test.False(t, found)
	})

	t.Run("exists reports what get would find", func(t *testing.T) {
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

			arguments, bindErr := statements["exists"].Bind(subjectKey(tc.subjectType, tc.subjectID))
			must.NoError(t, bindErr)

			var got bool

			must.NoError(t, db.QueryRowContext(ctx, statements["exists"].SQL, arguments...).Scan(&got))
			test.EqOp(t, tc.want, got, test.Sprintf("%s/%s", tc.subjectType, tc.subjectID))
		}
	})

	t.Run("an update writes the row its key names and no other", func(t *testing.T) {
		values := subjectKey("user", "s_001")
		values["key_material"] = "rotated"

		test.EqOp(t, int64(1), affectedRows(t, execBound(t, ctx, db, statements["update"], values)))

		material, found := getSubjectKey(t, ctx, db, statements, "user", "s_001")
		test.True(t, found)
		test.EqOp(t, "rotated", material)

		// The two rows sharing a column with it are untouched, which is the
		// assertion an update keyed on one half of the key would fail.
		material, found = getSubjectKey(t, ctx, db, statements, "device", "s_001")
		test.True(t, found)
		test.EqOp(t, "device s_001 material", material)

		material, found = getSubjectKey(t, ctx, db, statements, "user", "s_002")
		test.True(t, found)
		test.EqOp(t, "user s_002 material", material)
	})

	t.Run("archive soft-deletes once and reports the second attempt", func(t *testing.T) {
		values := subjectKey("user", "s_002")

		test.EqOp(t, int64(1), affectedRows(t, execBound(t, ctx, db, statements["archive"], values)))

		// The archived_at IS NULL that makes the second count zero survived
		// archiveStatement's move onto singleRowPredicates.
		test.EqOp(t, int64(0), affectedRows(t, execBound(t, ctx, db, statements["archive"], values)))

		_, found := getSubjectKey(t, ctx, db, statements, "user", "s_002")
		test.False(t, found)

		// And the row it shares a subject type with is still live.
		_, found = getSubjectKey(t, ctx, db, statements, "user", "s_001")
		test.True(t, found)
	})

	t.Run("an archived row is not updatable", func(t *testing.T) {
		values := subjectKey("user", "s_002")
		values["key_material"] = "should not land"

		test.EqOp(t, int64(0), affectedRows(t, execBound(t, ctx, db, statements["update"], values)))
	})
}
