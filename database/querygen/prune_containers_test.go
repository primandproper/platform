package querygen

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The prune is the shape whose three renderings are three statements, and a
// string comparison can only say that each one was rendered. What a server says
// is the part that matters: that a pass takes as many rows as it was allowed and
// no more, that it takes the oldest of them first, that the rows short of the
// horizon survive it, and that a key of two columns addresses rows by the pair
// rather than by either half.
//
// The suite stands up its own table, like every other suite hanging off
// runDialect, so its deletions cannot move what the others assert on.

// sweepingsDDL is what a retention pass runs against: rows that accrue and are
// removed once they age past a window. It carries a second key — the scope and
// the sequence within it — because the row-value comparison a compound key
// renders is the half of this shape a single-column key never exercises.
func sweepingsDDL(d dialect.Dialect) string {
	switch d {
	case dialect.MySQL:
		return fmt.Sprintf(`CREATE TABLE %s (
			id VARCHAR(64) NOT NULL PRIMARY KEY,
			scope VARCHAR(64) NOT NULL,
			seq BIGINT NOT NULL,
			recorded_at DATETIME(6) NOT NULL,
			UNIQUE (scope, seq)
		)`, sweepingsTable)
	case dialect.SQLite:
		return fmt.Sprintf(`CREATE TABLE %s (
			id TEXT NOT NULL PRIMARY KEY,
			scope TEXT NOT NULL,
			seq INTEGER NOT NULL,
			recorded_at TEXT NOT NULL,
			UNIQUE (scope, seq)
		)`, sweepingsTable)
	// Postgres, which For has already narrowed the alternatives to.
	default:
		return fmt.Sprintf(`CREATE TABLE %s (
			id TEXT NOT NULL PRIMARY KEY,
			scope TEXT NOT NULL,
			seq BIGINT NOT NULL,
			recorded_at TIMESTAMP WITH TIME ZONE NOT NULL,
			UNIQUE (scope, seq)
		)`, sweepingsTable)
	}
}

// sweepingsQueries is the pair the suite runs: the horizon sweep keyed on the
// id, and the scoped one keyed on the pair that identifies a row within a
// scope — audit's prune, which compares a sequence rather than an instant.
func sweepingsQueries(d dialect.Dialect) []*Query {
	g := For(d)

	return []*Query{
		g.PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()),
		g.PruneQuery("PruneSweepingsInScope", sweepingsTable,
			Prune{Key: []string{"scope", "seq"}, Order: []Order{{Column: "scope"}, {Column: "seq"}}},
			Match{Column: "scope"},
			Match{Column: "seq", Against: AtMostArgument, Arg: "horizon_seq"}),
	}
}

// insertSweeping writes one row. It is the suite's own scaffolding rather than
// something this package emits: the prune takes no column list, so there is no
// generated create to reach for here.
func insertSweeping(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, id, scope string, seq int, at time.Time) {
	tb.Helper()

	statement := fmt.Sprintf("INSERT INTO %s (id, scope, seq, recorded_at) VALUES (%s)",
		sweepingsTable, d.Placeholders(1, 4))

	_, err := db.ExecContext(ctx, statement, id, scope, seq, timeArg(d, at))
	must.NoError(tb, err)
}

// prune runs one pass and reports the rows it took, which is what a caller loops
// on.
func prune(tb testing.TB, ctx context.Context, d dialect.Dialect, db *sql.DB, name string, values map[string]any) int64 {
	tb.Helper()

	statement, order := bindArguments(d, named(tb, sweepingsQueries(d), name).Content)

	return affected(tb, ctx, db, statement, argumentsFor(tb, order, values))
}

// survivingSweepings reads back what a pass left, in id order.
func survivingSweepings(tb testing.TB, ctx context.Context, db *sql.DB) []string {
	tb.Helper()

	return scanIDs(tb, ctx, db, "SELECT id FROM "+sweepingsTable+" ORDER BY id", nil)
}

func runPruneSuite(t *testing.T, ctx context.Context, d dialect.Dialect, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, sweepingsDDL(d))
	must.NoError(t, err)

	t.Run("every prune is one the server accepts", func(t *testing.T) {
		for _, query := range sweepingsQueries(d) {
			prepare(t, ctx, d, db, query)
		}
	})

	now := time.Now().UTC()

	// Five rows a minute apart, written out of order so that nothing here can
	// pass by taking rows in the order they were inserted.
	accrued := []struct {
		id  string
		ago time.Duration
		seq int
	}{
		{id: "s_003", ago: 3 * time.Minute, seq: 3},
		{id: "s_001", ago: 5 * time.Minute, seq: 1},
		{id: "s_005", ago: 1 * time.Minute, seq: 5},
		{id: "s_002", ago: 4 * time.Minute, seq: 2},
		{id: "s_004", ago: 2 * time.Minute, seq: 4},
	}

	for i := range accrued {
		insertSweeping(t, ctx, d, db, accrued[i].id, "sweep", accrued[i].seq, now.Add(-accrued[i].ago))
	}

	// The line the sweep is drawn at. Three of the five rows are past it: the
	// ones recorded five, four and three minutes ago.
	horizon := map[string]any{horizonArg: timeArg(d, now.Add(-150*time.Second))}

	pass := func(limit int) int64 {
		values := map[string]any{LimitArg: limit}
		maps.Copy(values, horizon)

		return prune(t, ctx, d, db, "PruneSweepings", values)
	}

	t.Run("a pass takes the cap and stops, oldest first", func(t *testing.T) {
		// Both halves of the shape at once. Three rows are doomed and the cap
		// is two, so a pass that ignored the bound would take all three — which
		// is the statement whose locks a neglected table holds for minutes —
		// and one that ignored the ordering could take any two of them.
		test.EqOp(t, int64(2), pass(2))
		test.Eq(t, []string{"s_003", "s_004", "s_005"}, survivingSweepings(t, ctx, db))
	})

	t.Run("the count is what tells a caller to run again", func(t *testing.T) {
		// The second pass takes the one row left past the horizon and reports
		// fewer than the cap, which is how a loop learns the backlog is drained
		// without a second query asking.
		test.EqOp(t, int64(1), pass(2))
		test.EqOp(t, int64(0), pass(2))
	})

	t.Run("the rows short of the horizon survive every pass", func(t *testing.T) {
		// The inclusive boundary read from the other side: what remains is
		// exactly the pair the cutoff did not reach, however many times the
		// pass runs.
		test.Eq(t, []string{"s_004", "s_005"}, survivingSweepings(t, ctx, db))
	})

	t.Run("a compound key addresses rows by the pair", func(t *testing.T) {
		// The row-value comparison. Deleting where scope = 'a' and seq <= 2
		// must leave a/3 and b/1 alone — and a rendering that compared either
		// column on its own would take b/1 with them.
		scoped := []struct {
			id    string
			scope string
			seq   int
		}{
			{id: "p_a1", scope: "a", seq: 1},
			{id: "p_a2", scope: "a", seq: 2},
			{id: "p_a3", scope: "a", seq: 3},
			{id: "p_b1", scope: "b", seq: 1},
		}

		for i := range scoped {
			insertSweeping(t, ctx, d, db, scoped[i].id, scoped[i].scope, scoped[i].seq, now)
		}

		taken := prune(t, ctx, d, db, "PruneSweepingsInScope", map[string]any{
			"scope":       "a",
			"horizon_seq": 2,
			LimitArg:      5,
		})

		test.EqOp(t, int64(2), taken)
		test.Eq(t, []string{"p_a3", "p_b1", "s_004", "s_005"}, survivingSweepings(t, ctx, db))
	})
}
