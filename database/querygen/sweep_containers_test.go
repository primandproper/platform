package querygen

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The sweeps are the shapes whose whole promise is behavioral, and one of them
// is a shape MySQL refuses to run at all in its obvious spelling.
//
// A string comparison can say that a derived table was rendered for MySQL and
// not elsewhere. Only a server can say that the derived table is what makes the
// statement legal there, that the LIMIT actually bounds a pass, that the
// ordering makes it the *most overdue* rows a pass takes rather than whichever
// the planner reached first, and that the inclusive boundary collects the row
// sitting exactly on it. Every one of those fails here or nowhere.
//
// The suite stands up its own table, like every other suite hanging off
// runDialect, so its writes cannot move what the others assert on.

const sweepTable = "leases"

// leasesDDL is the shape a background pass runs over: a deadline, a state it
// moves through, and the convention triple.
//
// archived_at is here because one of the assertions is about what the column
// list does with it — the read excludes archived rows and the hard delete
// reaches them, from the same table, by being handed different lists.
func leasesDDL(d dialect.Dialect) string {
	switch d {
	case dialect.MySQL:
		return fmt.Sprintf(`CREATE TABLE %s (
			id VARCHAR(64) NOT NULL PRIMARY KEY,
			state VARCHAR(32) NOT NULL,
			expires_at DATETIME(6) NOT NULL,
			created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
			last_updated_at DATETIME(6) NULL,
			archived_at DATETIME(6) NULL
		)`, sweepTable)
	case dialect.SQLite:
		return fmt.Sprintf(`CREATE TABLE %s (
			id TEXT NOT NULL PRIMARY KEY,
			state TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_updated_at TEXT,
			archived_at TEXT
		)`, sweepTable)
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return fmt.Sprintf(`CREATE TABLE %s (
			id TEXT NOT NULL PRIMARY KEY,
			state TEXT NOT NULL,
			expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
			last_updated_at TIMESTAMP WITH TIME ZONE,
			archived_at TIMESTAMP WITH TIME ZONE
		)`, sweepTable)
	}
}

func leasesColumns() []string {
	return []string{
		IDColumn,
		"state",
		"expires_at",
		CreatedAtColumn,
		LastUpdatedAtColumn,
		ArchivedAtColumn,
	}
}

const expiresBeforeArg = "expires_before"

// dueByNow is the predicate all four statements share: a lease past a bound
// instant. It is AtMostArgument rather than CurrentTime because the suite drives the
// clock — which is the difference the two comparands exist for.
func dueByNow() Match {
	return Match{Column: "expires_at", Against: AtMostArgument, Arg: expiresBeforeArg}
}

// byExpiry is the order every pass walks: the deadline, then the id to settle
// the rows that came due in the same instant.
func byExpiry() []Order {
	return []Order{{Column: "expires_at"}, {Column: IDColumn}}
}

// leaseQueries is the set the suite runs: the bounded read, the bounded write of
// each kind, and the count beside them.
func leaseQueries(d dialect.Dialect) []*Query {
	g := For(d)

	return []*Query{
		g.InsertQuery("CreateLease", sweepTable, ForInsert(leasesColumns()), []string{ArchivedAtColumn}),

		// The read: the most overdue live leases in one state, no more than a
		// batch of them.
		g.SweepQuery("ListDueLeases", sweepTable, leasesColumns(),
			Sweep{Order: byExpiry(), Projection: []string{IDColumn}},
			Match{Column: "state"}, dueByNow()),

		// The bounded stamp, guarded on the state it is replacing so a pass
		// cannot move a lease somebody else has already dealt with.
		g.SweepUpdateQuery("ExpireDueLeases", sweepTable, leasesColumns(),
			[]string{"state"}, nil, byExpiry(),
			Match{Column: "state", Arg: "current_state"}, dueByNow()),

		// The bounded delete, over a column list without archived_at: a
		// retention pass that skipped the archived rows would leave behind
		// exactly the records nobody is looking at any more.
		g.SweepDeleteQuery("PurgeDueLeases", sweepTable,
			without(leasesColumns(), ArchivedAtColumn), byExpiry(),
			Match{Column: "state"}, dueByNow()),

		g.CountQuery("CountDueLeases", sweepTable, leasesColumns(), dueByNow()),
	}
}

// leaseQuery finds one of them and returns it bound.
func leaseQuery(tb testing.TB, d dialect.Dialect, name string, values map[string]any) (statement string, arguments []any) {
	tb.Helper()

	bound, order := bindArguments(d, named(tb, leaseQueries(d), name).Content)

	return bound, argumentsFor(tb, order, values)
}

// insertLease writes one row through the generated create.
func insertLease(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, id, state string, expiresAt time.Time) {
	tb.Helper()

	statement, arguments := leaseQuery(tb, d, "CreateLease", map[string]any{
		IDColumn:     id,
		"state":      state,
		"expires_at": timeArg(d, expiresAt),
	})

	_, err := db.ExecContext(ctx, statement, arguments...)
	must.NoError(tb, err, must.Sprintf("inserting %s", id))
}

// dueLeases runs the bounded read and returns the ids it named, in the order it
// named them.
func dueLeases(
	tb testing.TB,
	ctx context.Context,
	d dialect.Dialect,
	db *sql.DB,
	state string,
	at time.Time,
	limit int64,
) []string {
	tb.Helper()

	statement, arguments := leaseQuery(tb, d, "ListDueLeases", map[string]any{
		"state":          state,
		expiresBeforeArg: timeArg(d, at),
		LimitArg:         limit,
	})

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

// liveLeases reads the table back, archived rows included, so an assertion can
// say which rows survived a pass rather than only how many went.
func liveLeases(tb testing.TB, ctx context.Context, db *sql.DB) []string {
	tb.Helper()

	rows, err := db.QueryContext(ctx, "SELECT id FROM "+sweepTable+" ORDER BY id")
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

// runSweepSuite is written once and run against each of the three servers, like
// every other suite here.
func runSweepSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, leasesDDL(d))
	must.NoError(t, err)

	t.Run("every bounded statement is one the server accepts", func(t *testing.T) {
		// This is where MySQL's derived table earns its keep: the obvious
		// spelling of a bounded write parses on all three and is rejected at
		// execution by exactly one, with ER_UPDATE_TABLE_USED.
		for _, query := range leaseQueries(d) {
			prepare(t, ctx, d, db, query)
		}
	})

	// A whole second between each, so SQLite's second-granular stored timestamps
	// still order these the way the assertions read them.
	now := time.Now().UTC().Truncate(time.Second)

	// Inserted out of expiry order, so nothing can be relying on insertion
	// order for the ordering assertions below.
	insertLease(t, ctx, d, db, "l_003", "held", now.Add(-time.Second))
	insertLease(t, ctx, d, db, "l_001", "held", now.Add(-3*time.Second))
	insertLease(t, ctx, d, db, "l_004", "held", now)
	insertLease(t, ctx, d, db, "l_002", "held", now.Add(-2*time.Second))
	insertLease(t, ctx, d, db, "l_005", "held", now.Add(time.Hour))

	t.Run("the read takes the most overdue rows first", func(t *testing.T) {
		// The ordering is what makes "the next N" mean something. Without it a
		// bounded pass can take whichever rows the planner reached first and
		// step over the oldest row forever.
		test.Eq(t, []string{"l_001", "l_002", "l_003", "l_004"}, dueLeases(t, ctx, d, db, "held", now, 10))
	})

	t.Run("the limit bounds the pass", func(t *testing.T) {
		test.Eq(t, []string{"l_001", "l_002"}, dueLeases(t, ctx, d, db, "held", now, 2))
	})

	t.Run("the boundary is inclusive and its complement is not", func(t *testing.T) {
		// l_004 expires at exactly the instant asked about and is collected;
		// l_005 is an hour out and is not. That partition is what keeps a row
		// from being neither collected nor live at the instant its deadline
		// falls on.
		test.SliceContains(t, dueLeases(t, ctx, d, db, "held", now, 10), "l_004")
		test.SliceNotContains(t, dueLeases(t, ctx, d, db, "held", now, 10), "l_005")
	})

	t.Run("the bounded update moves the rows the read named", func(t *testing.T) {
		statement, arguments := leaseQuery(t, d, "ExpireDueLeases", map[string]any{
			"state":          "expired",
			"current_state":  "held",
			expiresBeforeArg: timeArg(d, now),
			LimitArg:         int64(2),
		})

		// Two, because the limit said two — and they are the two the read named
		// first, since both statements carry the same ordering.
		test.EqOp(t, int64(2), affected(t, ctx, db, statement, arguments))

		test.Eq(t, []string{"l_003", "l_004"}, dueLeases(t, ctx, d, db, "held", now, 10))
	})

	t.Run("the bounded update's guard refuses a row that moved on", func(t *testing.T) {
		// The rows it already expired are still past their deadline, so what
		// keeps a second pass from touching them is the state guard rather than
		// the horizon.
		statement, arguments := leaseQuery(t, d, "ExpireDueLeases", map[string]any{
			"state":          "expired",
			"current_state":  "released",
			expiresBeforeArg: timeArg(d, now),
			LimitArg:         int64(10),
		})

		test.EqOp(t, int64(0), affected(t, ctx, db, statement, arguments))
	})

	t.Run("the count answers for the rows the read would return", func(t *testing.T) {
		statement, arguments := leaseQuery(t, d, "CountDueLeases", map[string]any{
			expiresBeforeArg: timeArg(d, now),
		})

		var counted int64

		must.NoError(t, db.QueryRowContext(ctx, statement, arguments...).Scan(&counted))

		// The count has no limit on it, which is the point of asking: it is what
		// a gauge reports, where the read is what a pass acts on.
		test.EqOp(t, int64(4), counted)
	})

	t.Run("the bounded delete removes the rows its predicate names", func(t *testing.T) {
		statement, arguments := leaseQuery(t, d, "PurgeDueLeases", map[string]any{
			"state":          "expired",
			expiresBeforeArg: timeArg(d, now),
			LimitArg:         int64(10),
		})

		test.EqOp(t, int64(2), affected(t, ctx, db, statement, arguments))

		test.Eq(t, []string{"l_003", "l_004", "l_005"}, liveLeases(t, ctx, db))
	})

	t.Run("the bounded delete reaches an archived row", func(t *testing.T) {
		// Its column list has no archived_at in it, which is how a hard delete
		// says so — every other statement built on this key carries the archived
		// clause, so an archived lease is invisible to them.
		_, archiveErr := db.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET archived_at = %s WHERE id = %s",
				sweepTable, d.Placeholder(1), d.Placeholder(2)),
			timeArg(d, now), "l_003")
		must.NoError(t, archiveErr)

		// The read skips it, because its column list does carry the column.
		test.SliceNotContains(t, dueLeases(t, ctx, d, db, "held", now, 10), "l_003")

		statement, arguments := leaseQuery(t, d, "PurgeDueLeases", map[string]any{
			"state":          "held",
			expiresBeforeArg: timeArg(d, now),
			LimitArg:         int64(10),
		})

		test.EqOp(t, int64(2), affected(t, ctx, db, statement, arguments))

		test.Eq(t, []string{"l_005"}, liveLeases(t, ctx, db))
	})
}
