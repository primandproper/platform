package querygen

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v11/testutils/containers/pgtest"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The emitted SQL is only worth anything if a server accepts it, and half of
// what this package promises is behavioral rather than textual: that
// include_archived toggles something, that filtered_count does not move as a
// caller pages, that the reindex walk covers every live row exactly once in byte
// order. None of that is visible in a string comparison, so it is checked here,
// against a real Postgres.

const widgetsTable = "widgets"

// widgetsDDL is a table with every column this package has an opinion about.
const widgetsDDL = `CREATE TABLE widgets (
	id TEXT NOT NULL PRIMARY KEY,
	name TEXT NOT NULL,
	belongs_to_account TEXT NOT NULL,
	last_indexed_at TIMESTAMP WITH TIME ZONE,
	created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
	last_updated_at TIMESTAMP WITH TIME ZONE,
	archived_at TIMESTAMP WITH TIME ZONE
)`

func widgetsColumns() []string {
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

func widgetsQueries() []*Query {
	return StandardCRUD(widgetsTable, widgetsColumns(),
		WithEntity("Widget", "Widgets"),
		WithOwnership(BelongsToAccountColumn),
	)
}

// sqlcArgument matches a sqlc argument reference in emitted SQL.
var sqlcArgument = regexp.MustCompile(`sqlc\.n?arg\(([a-zA-Z0-9_]+)\)`)

// bind rewrites sqlc argument references into numbered placeholders, returning
// the statement and the argument names in placeholder order.
//
// It is what sqlc itself does to a query before handing it to the driver, and
// doing it here is what lets these tests run the generated text rather than a
// paraphrase of it. Repeated references to one name collapse onto one
// placeholder, as they must: a filter's created_after appears in the outer WHERE
// and again inside the count beside it, and they are one argument.
func bind(statement string) (bound string, order []string) {
	positions := map[string]int{}

	bound = sqlcArgument.ReplaceAllStringFunc(statement, func(match string) string {
		name := sqlcArgument.FindStringSubmatch(match)[1]

		if _, ok := positions[name]; !ok {
			order = append(order, name)
			positions[name] = len(order)
		}

		return "$" + strconv.Itoa(positions[name])
	})

	return bound, order
}

// argumentsFor lines values up with the placeholders bind produced, failing the
// test when the statement wants an argument the caller did not supply.
func argumentsFor(tb testing.TB, order []string, values map[string]any) []any {
	tb.Helper()

	out := make([]any, 0, len(order))

	for _, name := range order {
		value, ok := values[name]
		must.True(tb, ok, must.Sprintf("statement wants an argument named %q", name))
		out = append(out, value)
	}

	return out
}

// widgetQuery finds one of the generated queries and returns it bound.
func widgetQuery(tb testing.TB, name string) (bound string, order []string) {
	tb.Helper()

	return bind(named(tb, widgetsQueries(), name).Content)
}

// filterDefaults is a QueryFilter that filters nothing: every window unset,
// archived excluded, the first page.
func filterDefaults() map[string]any {
	return map[string]any{
		CreatedAfterArg:    nil,
		CreatedBeforeArg:   nil,
		UpdatedAfterArg:    nil,
		UpdatedBeforeArg:   nil,
		IncludeArchivedArg: nil,
		CursorArg:          nil,
		LimitArg:           nil,
	}
}

const testAccount = "account_one"

// insertWidget runs the generated create statement.
func insertWidget(tb testing.TB, ctx context.Context, db *sql.DB, id, name, account string) {
	tb.Helper()

	statement, order := widgetQuery(tb, "CreateWidget")

	_, err := db.ExecContext(ctx, statement, argumentsFor(tb, order, map[string]any{
		IDColumn:               id,
		"name":                 name,
		BelongsToAccountColumn: account,
	})...)
	must.NoError(tb, err)
}

// scanIDs runs a statement whose rows are a single id column.
func scanIDs(tb testing.TB, ctx context.Context, db *sql.DB, statement string, arguments []any) []string {
	tb.Helper()

	rows, err := db.QueryContext(ctx, statement, arguments...)
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	var ids []string

	for rows.Next() {
		var id string

		must.NoError(tb, rows.Scan(&id))

		ids = append(ids, id)
	}

	must.NoError(tb, rows.Err())

	return ids
}

// listWidgets runs the generated list statement, returning the ids it read and
// the two counts it reported.
func listWidgets(tb testing.TB, ctx context.Context, db *sql.DB, values map[string]any) (ids []string, filtered, total int64) {
	tb.Helper()

	statement, order := widgetQuery(tb, "ListWidgets")

	rows, err := db.QueryContext(ctx, statement, argumentsFor(tb, order, values)...)
	must.NoError(tb, err)

	defer func() { must.NoError(tb, rows.Close()) }()

	for rows.Next() {
		var (
			id, name, account          string
			indexed, updated, archived sql.NullTime
			created                    time.Time
			rowFiltered, rowTotal      int64
		)

		must.NoError(tb, rows.Scan(&id, &name, &account, &indexed, &created, &updated, &archived, &rowFiltered, &rowTotal))

		ids = append(ids, id)
		filtered, total = rowFiltered, rowTotal
	}

	must.NoError(tb, rows.Err())

	return ids, filtered, total
}

func TestQuerygen_Postgres(T *testing.T) {
	T.Parallel()

	pgtest.Run(T, func(ctx context.Context, pg *pgtest.Instance) {
		_, err := pg.DB.ExecContext(ctx, widgetsDDL)
		must.NoError(T, err)

		// Neither subtest is parallel, and the suite's own children are not
		// either: they share one table and one another's writes are what they
		// assert against.
		//nolint:paralleltest // sequential against a shared table, deliberately.
		T.Run("every generated statement is one the server accepts", func(t *testing.T) {
			for _, query := range widgetsQueries() {
				prepare(t, ctx, pg.DB, query)
			}
		})

		//nolint:paralleltest // sequential against a shared table, deliberately.
		T.Run("the suite", func(t *testing.T) {
			runWidgetSuite(t, ctx, pg.DB)
		})
	})
}

// prepare asks the server to plan the statement, which is the cheapest way to
// learn that every column it names exists and every argument it binds has a type
// Postgres can infer.
func prepare(tb testing.TB, ctx context.Context, db *sql.DB, query *Query) {
	tb.Helper()

	statement, _ := bind(query.Content)

	stmt, err := db.PrepareContext(ctx, statement)
	must.NoError(tb, err, must.Sprintf("preparing %s", query.Annotation.Name))
	must.NoError(tb, stmt.Close()) //nolint:sqlclosecheck // the prepare is the assertion; there is nothing to read.
}

func runWidgetSuite(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	// Ids are compared as bytes by the reindex walk, so they are inserted out of
	// order to make sure nothing is relying on insertion order.
	for _, id := range []string{"w_003", "w_001", "w_004", "w_002"} {
		insertWidget(t, ctx, db, id, "widget "+id, testAccount)
	}

	insertWidget(t, ctx, db, "w_005", "someone else's widget", "account_two")

	t.Run("archive soft-deletes once and reports the second attempt", func(t *testing.T) {
		statement, order := widgetQuery(t, "ArchiveWidget")
		arguments := argumentsFor(t, order, map[string]any{
			IDColumn:               "w_004",
			BelongsToAccountColumn: testAccount,
		})

		result, err := db.ExecContext(ctx, statement, arguments...)
		must.NoError(t, err)

		affected, err := result.RowsAffected()
		must.NoError(t, err)
		test.EqOp(t, int64(1), affected)

		// The archived_at IS NULL in the WHERE is what makes this second count
		// zero rather than one, and it is the only thing telling a caller their
		// archival was a no-op.
		result, err = db.ExecContext(ctx, statement, arguments...)
		must.NoError(t, err)

		affected, err = result.RowsAffected()
		must.NoError(t, err)
		test.EqOp(t, int64(0), affected)
	})

	t.Run("get and exists refuse a row belonging to another account", func(t *testing.T) {
		getStatement, getOrder := widgetQuery(t, "GetWidget")

		row := db.QueryRowContext(ctx, getStatement, argumentsFor(t, getOrder, map[string]any{
			IDColumn:               "w_005",
			BelongsToAccountColumn: testAccount,
		})...)

		var id string
		test.ErrorIs(t, row.Scan(&id), sql.ErrNoRows)

		existsStatement, existsOrder := widgetQuery(t, "CheckWidgetExistence")

		var exists bool
		must.NoError(t, db.QueryRowContext(ctx, existsStatement, argumentsFor(t, existsOrder, map[string]any{
			IDColumn:               "w_005",
			BelongsToAccountColumn: testAccount,
		})...).Scan(&exists))
		test.False(t, exists)
	})

	t.Run("include_archived actually includes archived rows", func(t *testing.T) {
		values := filterDefaults()
		values[BelongsToAccountColumn] = testAccount

		excluded, _, _ := listWidgets(t, ctx, db, values)
		test.Eq(t, []string{"w_001", "w_002", "w_003"}, excluded)

		values[IncludeArchivedArg] = true

		included, _, _ := listWidgets(t, ctx, db, values)
		test.Eq(t, []string{"w_001", "w_002", "w_003", "w_004"}, included)
	})

	t.Run("the counts describe the filter rather than the page", func(t *testing.T) {
		values := filterDefaults()
		values[BelongsToAccountColumn] = testAccount
		values[LimitArg] = 2

		first, filtered, total := listWidgets(t, ctx, db, values)
		test.Eq(t, []string{"w_001", "w_002"}, first)
		test.EqOp(t, int64(3), filtered)
		test.EqOp(t, int64(3), total)

		values[CursorArg] = first[len(first)-1]

		second, filteredAgain, totalAgain := listWidgets(t, ctx, db, values)
		test.Eq(t, []string{"w_003"}, second)

		// A count that moved with the cursor would report two here, then one,
		// then zero — a total that empties itself as the caller reads it.
		test.EqOp(t, filtered, filteredAgain)
		test.EqOp(t, total, totalAgain)
	})

	t.Run("the keyset walk reads every row exactly once", func(t *testing.T) {
		values := filterDefaults()
		values[BelongsToAccountColumn] = testAccount
		values[LimitArg] = 1

		var walked []string

		for range 10 {
			page, _, _ := listWidgets(t, ctx, db, values)
			if len(page) == 0 {
				break
			}

			walked = append(walked, page...)
			values[CursorArg] = page[len(page)-1]
		}

		test.Eq(t, []string{"w_001", "w_002", "w_003"}, walked)
	})

	t.Run("the created window bounds the page and the count together", func(t *testing.T) {
		values := filterDefaults()
		values[BelongsToAccountColumn] = testAccount
		values[CreatedAfterArg] = time.Now().Add(time.Hour)

		ids, filtered, total := listWidgets(t, ctx, db, values)

		test.SliceEmpty(t, ids)
		// Both counts ride on the rows, so an empty page carries neither. It is
		// the caller who supplies the zero — which is why
		// filtering.NewQueryFilteredResult takes the counts as arguments rather
		// than reading them off the data.
		test.EqOp(t, int64(0), filtered)
		test.EqOp(t, int64(0), total)

		// Widen the window back and the pair is legible again: everything
		// matched, out of everything.
		values[CreatedAfterArg] = nil

		ids, filtered, total = listWidgets(t, ctx, db, values)

		test.SliceLen(t, 3, ids)
		test.EqOp(t, int64(3), filtered)
		test.EqOp(t, int64(3), total)
	})

	t.Run("update stamps last_updated_at and leaves the owner alone", func(t *testing.T) {
		statement, order := widgetQuery(t, "UpdateWidget")

		result, err := db.ExecContext(ctx, statement, argumentsFor(t, order, map[string]any{
			IDColumn:               "w_001",
			"name":                 "renamed",
			BelongsToAccountColumn: testAccount,
		})...)
		must.NoError(t, err)

		affected, err := result.RowsAffected()
		must.NoError(t, err)
		test.EqOp(t, int64(1), affected)

		var (
			name    string
			account string
			updated sql.NullTime
		)

		must.NoError(t, db.QueryRowContext(ctx,
			"SELECT name, belongs_to_account, last_updated_at FROM widgets WHERE id = $1", "w_001",
		).Scan(&name, &account, &updated))

		test.EqOp(t, "renamed", name)
		test.EqOp(t, testAccount, account)
		test.True(t, updated.Valid)
	})

	t.Run("the reindex scan walks live ids in byte order", func(t *testing.T) {
		statement, order := widgetQuery(t, "ScanWidgetIDsForReindex")

		var walked []string

		cursor := ""

		for range 10 {
			page := scanIDs(t, ctx, db, statement, argumentsFor(t, order, map[string]any{
				CursorArg: cursor,
				LimitArg:  2,
			}))

			if len(page) == 0 {
				break
			}

			walked = append(walked, page...)
			cursor = page[len(page)-1]
		}

		// Every unarchived row, from both accounts — a reindex is not scoped —
		// and w_004 absent because it was archived.
		test.Eq(t, []string{"w_001", "w_002", "w_003", "w_005"}, walked)
	})

	t.Run("create refuses to supply a database-owned column", func(t *testing.T) {
		// Nothing here asserts on SQL text: the point is that the generated
		// insert leaves created_at to the server, so a row exists with a
		// creation time nobody passed in.
		insertWidget(t, ctx, db, "w_006", "later", testAccount)

		var created time.Time
		must.NoError(t, db.QueryRowContext(ctx,
			fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", CreatedAtColumn, widgetsTable, IDColumn), "w_006",
		).Scan(&created))

		test.False(t, created.IsZero())
	})
}
