package querygen

import (
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// ErrDegeneratePrune indicates a prune that is not one, in either of the two
// ways a caller can ask for that: no key, so nothing names the rows the capped
// read chose and the DELETE has no comparison to make; or no predicate, so
// every row in the table is past the horizon and the statement is a truncate
// run a batch at a time.
//
// Each is a programming error rather than a caller's — nothing on a request
// path decides what a statement dooms — so it panics like the rest of this
// package's misuse. The wrapped message says which of the two it was.
var ErrDegeneratePrune = platformerrors.New("prune would doom rows it cannot name")

// doomedAlias is what the capped read calls the table inside a prune.
//
// It is an alias rather than a bare self-reference because SQLite needs one:
// a subquery selecting from the table its DELETE is deleting from leaves every
// column name reachable from two places, and the failure is an
// ambiguous-column error at run time rather than a parse failure a generator
// would have seen. Postgres does not need it and gets it anyway, because one
// rendering is one thing to get right.
const doomedAlias = "doomed"

// Prune is what a bounded delete needs beyond the predicates that doom a row:
// the columns naming a doomed row, and the order the capped pass takes them in.
type Prune struct {
	// Key names the columns that address a doomed row — the id of a
	// conventional table, or every column of a natural key.
	//
	// It is what the capped read projects and what the DELETE compares
	// against, and it is required on every dialect including the one that
	// renders neither. MySQL caps the DELETE itself and never names a key at
	// all; a corpus is authored once and rendered three times, so a field one
	// arm ignores is still the field that decides whether the other two have a
	// statement.
	//
	// More than one column renders a row-value comparison — `(a, b) IN (SELECT
	// d.a, d.b …)` — which is the queue tables' shape, where (queue_name,
	// item_key) names a row and neither half of it does.
	//
	// The names are interpolated, so they are restricted rather than escaped —
	// see dialect.ValidIdentifier.
	Key []string
	// Order is the order the doomed rows are chosen in, and naming none is
	// legitimate in the way an unpaged list's is: a pass then takes whichever
	// of the matched rows the planner reached first, which for a horizon sweep
	// costs nothing, since every row it could have taken instead is still past
	// the horizon on the next pass.
	//
	// Two callers want it named anyway. A sweep ordered by the column its
	// horizon compares against takes the oldest rows first, so a backlog drains
	// in the order it accumulated rather than in the order a plan happened to
	// produce. And a reaper sharing its table with keyed writers wants the
	// primary key's order, because taking row locks in the order every other
	// writer takes them is what keeps a pass out of a deadlock.
	Order []Order
}

// PruneQuery renders the delete a retention pass runs: the rows its predicates
// doom, capped so that one pass touches a bounded number of them.
//
// The cap is the whole shape. A table nobody has swept for a month holds a
// month of rows past its horizon, and the unbounded DELETE that clears them is
// one statement holding locks for minutes, replicating as one transaction, and
// timing out somewhere in the middle — after which the next attempt starts from
// the beginning. A capped pass is a loop the caller owns instead: it deletes as
// many rows as it was allowed, reports how many that was, and runs again while
// the count says there are more. That is why this is annotated :execrows. The
// count is not a courtesy here, it is the loop's condition.
//
// # Two grammars, and neither one travels
//
// MySQL caps the DELETE itself, with the ORDER BY and LIMIT its own grammar
// takes. Postgres and SQLite have no DELETE … LIMIT, so the bound goes on a
// read instead: a capped SELECT names the doomed rows and the DELETE removes
// whatever it named.
//
// Neither arm is a preference the other dialect could adopt. MySQL refuses a
// subquery that reads the table being deleted from — ER_UPDATE_TABLE_USED,
// error 1093 — so the doomed-subquery form is not available there at all;
// Postgres does not parse DELETE … LIMIT, and SQLite parses it only in builds
// compiled with an option most are not, which is a failure that waits until run
// time. So this is one of the places the three disagree about a statement's
// shape rather than about an expression inside one, and it is confined to
// Generator.boundedDelete the way the upsert's is to Generator.conflictHeader.
//
// # Locked where the dialect locks
//
// The capped read takes FOR UPDATE SKIP LOCKED on Postgres, which is what lets
// a fleet prune one table at once: each pass locks the batch it chose and skips
// whatever another holds, so two pruners take disjoint batches instead of
// queueing behind each other. A row skipped is still past the horizon on the
// next pass, which is what a reaper can afford and a claim cannot — it is the
// one writer with nothing to prove.
//
// SQLite has no FOR UPDATE, and its absence there is correct rather than
// missing: one writer at a time is the whole storage model, so there is nothing
// to skip and the capped read is the degenerate unlocked one. MySQL's arm has
// nowhere to put a lock clause, since the DELETE itself carries the bound — so
// two pruners racing there serialize on the rows they both chose rather than
// dividing them. Every pass stays bounded and correct on all three; what the
// grammar decides is throughput under contention.
//
// # What it dooms
//
// The matches are the horizon — a timestamp at or before a bound cutoff, a
// completed_at that is not null, the queue whose backlog is being reaped — and
// the shape refuses to be handed none of them. A prune with no predicate dooms
// every row in the table, which is a truncate run a batch at a time, so it is
// [ErrDegeneratePrune] rather than a statement that empties a table one pass at
// a time until somebody notices.
//
// There is no archived predicate, for [Generator.DeleteQuery]'s reason: the row
// is being destroyed rather than hidden, and a row archived a year ago is
// precisely the row a retention pass exists to remove.
//
// The cap binds under [LimitArg] on all three dialects, and it is required. An
// absent cap is the unbounded statement this shape exists to make unspellable,
// so it has no default the way a page size does — see Generator.limitClause for
// the one thing that does differ, which is that MySQL's grammar takes a bare
// placeholder after LIMIT and has nowhere to put the name.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
//
// It panics rather than returning an error, in the manner of the rest of this
// package: its arguments are string literals in a generator binary. The panic
// value is an error wrapping dialect.ErrInvalidIdentifier or
// [ErrDegeneratePrune].
func (g *Generator) PruneQuery(name, table string, prune Prune, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content:    g.pruneStatement(table, prune, matches),
	}
}

// pruneStatement checks what the two arms both assume and hands the rendering
// to the dialect, which is where the shapes part company.
func (g *Generator) pruneStatement(table string, prune Prune, matches []Match) string {
	mustIdentifier("table name", table)

	if len(prune.Key) == 0 {
		panic(platformerrors.Wrapf(ErrDegeneratePrune, "querygen: table %q names no key", table))
	}

	for _, column := range prune.Key {
		mustIdentifier("prune key column", column)
	}

	if len(matches) == 0 {
		panic(platformerrors.Wrapf(ErrDegeneratePrune, "querygen: table %q names no predicate", table))
	}

	return g.boundedDelete(table, prune, matches)
}

// keyComparand renders the key as the left side of the DELETE's comparison: the
// column alone where the key is one column, and a row value where it is several.
//
// It is unqualified, as the predicates of every write verb here are: the DELETE
// names one table, and the only other occurrence of it in the statement is the
// capped read's, which is aliased.
//
// The parentheses are omitted for a single column rather than written
// harmlessly, because `(a)` is a parenthesized expression on all three servers
// and a row value on none of them — so a one-column key wrapped that way is a
// statement whose meaning happens to survive its punctuation, which is not a
// thing to leave in text three analyzers read.
func keyComparand(key []string) string {
	if len(key) == 1 {
		return key[0]
	}

	return "(" + strings.Join(key, ", ") + ")"
}
