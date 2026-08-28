package querygen

import (
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v13/database/dialect"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// The prune is the one shape whose three renderings are three statements rather
// than one statement with a substituted expression. So the assertions below come
// in pairs: what every dialect promises — the same name, the same arguments, the
// same rows doomed — and what each one's grammar makes of it.

// sweepingsTable is the shape a retention pass runs against: rows that accrue,
// carry the instant they accrued at, and are removed once they are older than a
// window the caller keeps.
const sweepingsTable = "sweepings"

// horizonArg is what the cutoff binds under. It is named rather than defaulted
// to the column, because "recorded_at" reads as the value in the row and this is
// the line the sweep is drawn at.
const horizonArg = "horizon"

// pastHorizon is the predicate every prune below dooms rows with.
func pastHorizon() Match {
	return Match{Column: "recorded_at", Against: AtMostArgument, Arg: horizonArg}
}

// oldestFirst is the ordering a horizon sweep wants: a backlog drains in the
// order it accumulated.
func oldestFirst() Prune {
	return Prune{Key: []string{IDColumn}, Order: []Order{{Column: "recorded_at"}}}
}

func TestGenerator_PruneQuery(T *testing.T) {
	T.Parallel()

	T.Run("annotates the statement as an execrows", func(t *testing.T) {
		t.Parallel()

		// The count is the loop's condition rather than a courtesy: a caller
		// runs the pass again while it says there are more rows to take.
		for _, d := range everyDialect() {
			query := For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon())

			test.EqOp(t, "PruneSweepings", query.Annotation.Name, test.Sprintf("dialect %q", d))
			test.EqOp(t, ExecRowsType, query.Annotation.Type, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("caps the DELETE itself where the grammar takes a LIMIT", func(t *testing.T) {
		t.Parallel()

		// MySQL is the arm that reads like what it does, and the predicates are
		// unqualified as every other write verb's here are.
		query := For(dialect.MySQL).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon())

		test.EqOp(t, "DELETE FROM sweepings\n"+
			"WHERE recorded_at <= sqlc.arg(horizon)\n"+
			"ORDER BY recorded_at ASC\n"+
			"LIMIT ?;", query.Content)
	})

	T.Run("deletes the rows a capped read chose where the grammar has none", func(t *testing.T) {
		t.Parallel()

		// Postgres and SQLite have no DELETE ... LIMIT, so the bound goes on a
		// read and the DELETE takes whatever it named. The two differ in the
		// lock and in nothing else.
		test.EqOp(t, "DELETE FROM sweepings\n"+
			"WHERE id IN (\n"+
			"\tSELECT doomed.id\n"+
			"\tFROM sweepings AS doomed\n"+
			"\tWHERE doomed.recorded_at <= sqlc.arg(horizon)\n"+
			"\tORDER BY doomed.recorded_at ASC\n"+
			"\tLIMIT sqlc.arg(result_limit)\n"+
			"\tFOR UPDATE SKIP LOCKED\n"+
			");",
			For(dialect.Postgres).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content)

		test.EqOp(t, "DELETE FROM sweepings\n"+
			"WHERE id IN (\n"+
			"\tSELECT doomed.id\n"+
			"\tFROM sweepings AS doomed\n"+
			"\tWHERE doomed.recorded_at <= sqlc.arg(horizon)\n"+
			"\tORDER BY doomed.recorded_at ASC\n"+
			"\tLIMIT sqlc.arg(result_limit)\n"+
			");",
			For(dialect.SQLite).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content)
	})

	T.Run("locks the capped read where the dialect locks", func(t *testing.T) {
		t.Parallel()

		// Postgres skips what another pruner holds, so a fleet takes disjoint
		// batches. SQLite has no FOR UPDATE and needs none — one writer at a
		// time is the storage model — and MySQL's arm has nowhere to put a lock
		// clause, since the DELETE itself carries the bound.
		const lock = "FOR UPDATE SKIP LOCKED"

		test.StrContains(t,
			For(dialect.Postgres).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content, lock)

		for _, d := range []dialect.Dialect{dialect.MySQL, dialect.SQLite} {
			test.StrNotContains(t,
				For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content, lock,
				test.Sprintf("dialect %q", d))
		}
	})

	T.Run("aliases the table the capped read selects from", func(t *testing.T) {
		t.Parallel()

		// Not decoration: an unaliased self-reference leaves SQLite unable to
		// say which of the two occurrences a column belongs to, and it reports
		// that when the statement runs rather than when it is parsed.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			content := For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content

			test.StrContains(t, content, "FROM sweepings AS doomed", test.Sprintf("dialect %q", d))
			test.StrContains(t, content, "doomed.recorded_at", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("compares a compound key as a row value and a single one as a column", func(t *testing.T) {
		t.Parallel()

		// The queue tables' shape: (queue_name, item_key) names a row and
		// neither half of it does. A one-column key is not wrapped, because
		// `(a)` is a parenthesized expression on all three servers and a row
		// value on none of them.
		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.SQLite} {
			pair := For(d).PruneQuery("ReapItems", "workqueue_items",
				Prune{Key: []string{"queue_name", "item_key"}},
				Match{Column: "completed_at", Against: NoValue, Exclude: true}).Content

			test.StrContains(t, pair, "WHERE (queue_name, item_key) IN (", test.Sprintf("dialect %q", d))
			test.StrContains(t, pair, "SELECT doomed.queue_name, doomed.item_key", test.Sprintf("dialect %q", d))

			single := For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content

			test.StrContains(t, single, "WHERE id IN (", test.Sprintf("dialect %q", d))
			test.StrNotContains(t, single, "(id)", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("orders only where the caller named an order", func(t *testing.T) {
		t.Parallel()

		// An unordered pass takes whichever matched rows the planner reached
		// first, which costs a horizon sweep nothing: every row it could have
		// taken instead is still past the horizon next time.
		for _, d := range everyDialect() {
			content := For(d).PruneQuery("PruneSweepings", sweepingsTable,
				Prune{Key: []string{IDColumn}}, pastHorizon()).Content

			test.StrNotContains(t, content, "ORDER BY", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("binds the horizon and the cap, in that order, on every dialect", func(t *testing.T) {
		t.Parallel()

		// The cap is [LimitArg] on all three, because "how many rows may this
		// statement touch" is one question however the statement got there.
		// MySQL's marker carries no name — its grammar has nowhere to put one —
		// and bindArguments records it under the same key regardless.
		for _, d := range everyDialect() {
			_, args := bindArguments(d, For(d).PruneQuery("PruneSweepings", sweepingsTable,
				oldestFirst(), pastHorizon()).Content)

			test.Eq(t, []string{horizonArg, LimitArg}, args, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("caps every rendering", func(t *testing.T) {
		t.Parallel()

		// The bound is the shape. A rendering that lost it would be an
		// unbounded DELETE wearing a prune's name, which is the statement this
		// exists to replace.
		for _, d := range everyDialect() {
			content := For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content

			test.EqOp(t, 1, strings.Count(content, "LIMIT "), test.Sprintf("dialect %q", d))
		}
	})

	T.Run("renders no archived predicate", func(t *testing.T) {
		t.Parallel()

		// For the hard delete's reason: the row is being destroyed rather than
		// hidden, and a row archived a year ago is precisely the row a
		// retention pass exists to remove. There is no column list here for one
		// to be derived from, which is what makes the absence structural.
		for _, d := range everyDialect() {
			content := For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst(), pastHorizon()).Content

			test.StrNotContains(t, content, ArchivedAtColumn, test.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses a prune that names no key", func(t *testing.T) {
		t.Parallel()

		// Two of the three dialects have nothing to name the doomed rows with,
		// and the third would render a statement its siblings cannot. A corpus
		// is authored once and rendered three times, so the refusal is on all
		// of them.
		for _, d := range everyDialect() {
			err := recovered(func() {
				For(d).PruneQuery("PruneSweepings", sweepingsTable, Prune{}, pastHorizon())
			})

			must.ErrorIs(t, err, ErrDegeneratePrune, must.Sprintf("dialect %q", d))
			test.StrContains(t, err.Error(), sweepingsTable, test.Sprintf("dialect %q", d))
			test.StrContains(t, err.Error(), "no key", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses a prune that names no predicate", func(t *testing.T) {
		t.Parallel()

		// Without the refusal this is a truncate run a batch at a time: every
		// row in the table is doomed, and the only thing deciding how long that
		// takes is how often the pass runs.
		for _, d := range everyDialect() {
			err := recovered(func() {
				For(d).PruneQuery("PruneSweepings", sweepingsTable, oldestFirst())
			})

			must.ErrorIs(t, err, ErrDegeneratePrune, must.Sprintf("dialect %q", d))
			test.StrContains(t, err.Error(), "no predicate", test.Sprintf("dialect %q", d))
		}
	})

	T.Run("refuses an identifier it cannot interpolate", func(t *testing.T) {
		t.Parallel()

		// The table, the key and the ordering are all interpolated, so all
		// three are restricted rather than escaped.
		for name, render := range map[string]func(){
			"table": func() {
				For(dialect.Postgres).PruneQuery("PruneSweepings", "sweepings; DROP TABLE users", oldestFirst(), pastHorizon())
			},
			"key column": func() {
				For(dialect.Postgres).PruneQuery("PruneSweepings", sweepingsTable,
					Prune{Key: []string{"id, 1=1"}}, pastHorizon())
			},
			"order column": func() {
				For(dialect.Postgres).PruneQuery("PruneSweepings", sweepingsTable,
					Prune{Key: []string{IDColumn}, Order: []Order{{Column: "recorded_at)"}}}, pastHorizon())
			},
		} {
			err := recovered(render)

			must.ErrorIs(t, err, dialect.ErrInvalidIdentifier, must.Sprintf("interpolated %s", name))
		}
	})
}
