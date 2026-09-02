package querygen

import (
	"strings"

	platformerrors "github.com/primandproper/platform-go/v14/errors"
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
	// Order is the order the doomed rows are chosen in, and it is required —
	// see [ErrUnorderedBoundedStatement], which is the same requirement the
	// sweeps carry and is argued there once for both.
	//
	// Two orderings have callers here. A pass ordered by the column its horizon
	// compares against takes the oldest rows first, so a backlog drains in the
	// order it accumulated and its age is a number somebody can watch. And a
	// reaper sharing its table with keyed writers wants the primary key's
	// order, because taking row locks in the order every other writer takes
	// them is what keeps a pass out of a deadlock.
	Order []Order
	// Conditions are predicates this shape has no spelling for, rendered
	// beside the ones [Match] renders and joined to them by AND.
	//
	// A doom is usually a horizon or an equality, which a Match says. metering's
	// retention pass is the one that is not: an event row may be destroyed only
	// once the period it was folded into owes the provider nothing, and that is
	// a correlated NOT EXISTS over a second table rather than a comparison of a
	// column against a value. Rendering it would need an expression language
	// here, which the closed [Comparand] set exists to refuse — but the shape
	// around it is a bounded delete like any other, and a caller sent away to
	// write the whole statement out would be a caller writing down which of the
	// three spellings their server takes.
	//
	// So the predicate is the caller's and the statement is still this one:
	// same cap, same ordering, same per-dialect arm, same :execrows count the
	// pass loops on. What a condition gives up is the guarantee that its
	// predicate was derived rather than remembered, which is what every
	// authored statement in a corpus gives up.
	//
	// They are rendered verbatim, so a condition naming a column of the pruned
	// table qualifies it with [Generator.PruneQualifier] — which name that is,
	// is the dialect's answer rather than the caller's, since one arm bounds a
	// read of the table under an alias and the other bounds the DELETE itself.
	//
	// A condition is a narrowing beside the horizon rather than a substitute
	// for one: a prune whose only predicates were authored is still
	// [ErrDegeneratePrune], because what makes a pass a retention pass is a
	// horizon this package can see.
	Conditions []string
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
// # Two grammars, and one of them is a choice
//
// MySQL caps the DELETE itself, with the ORDER BY and LIMIT its own grammar
// takes. Postgres and SQLite have no DELETE … LIMIT, so the bound goes on a
// read instead: a capped SELECT names the doomed rows and the DELETE removes
// whatever it named.
//
// One of those arms is forced and the other is not, and it is worth being exact
// about which. Postgres does not parse DELETE … LIMIT at all, and SQLite parses
// it only in builds compiled with SQLITE_ENABLE_UPDATE_DELETE_LIMIT, which most
// are not — a failure that waits until run time — so the doomed subquery is the
// only bounded delete those two have. MySQL is the one with a choice: it refuses
// a subquery that reads the table being deleted from (ER_UPDATE_TABLE_USED,
// error 1093), but it accepts the identical rows once that scan is materialized
// through a derived table, which is the spelling [Generator.SweepDeleteQuery]
// renders there and dataprivacy's MySQL corpus executes. boundedWriteForm is
// where the three spellings and the servers that take them are written down.
//
// This shape declines the derived table because the native arm is strictly
// better for what a prune is: no materialization, no second projection, and the
// key columns — which on MySQL are never rendered at all — cost nothing. What it
// gives up is the property the sweep is buying, that one scan serves a read and
// two writes; a prune has no read to keep in step with. So the divergence is
// confined to Generator.boundedDelete the way the upsert's is to
// Generator.conflictHeader, and the fact underneath it is not confined at all —
// it is one table both shapes derive from.
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
// so it has no default the way a page size does — see Generator.capClause, and
// Generator.boundedLimit beneath it for the one thing that does differ, which is
// that MySQL's grammar takes a bare placeholder after LIMIT and has nowhere to
// put the name.
//
// The ordering is required for the reason [ErrUnorderedBoundedStatement] gives,
// which is the reason the sweeps require theirs: one answer, argued in one
// place, for every bounded statement this package renders.
//
// # This or the sweep
//
// [Generator.SweepDeleteQuery] is the other bounded delete here. Take this one
// for a retention pass over an append-only table, or for any pass whose rows are
// addressed by something other than an id: the key may be a natural key of
// several columns, archived rows are doomed like any others, and Postgres gets
// the lock clause that lets a fleet of reapers divide a backlog. Take the sweep
// where the rows are addressed by id, archived rows must be left alone, and the
// same scan has to serve a read or an update as well. The package comment's
// "Choosing between the prune and the sweep" works through the reapers this
// module already has.
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

// PruneQualifier names the pruned table the way [Generator.PruneQuery]'s own
// predicates name it on this dialect, so that a [Prune.Conditions] entry and
// the predicates beside it address one table under one name.
//
// The two arms disagree about what that name is, and neither is a preference.
// Where the bound goes on a read, the doomed rows are scanned under an alias —
// SQLite resolves a bare column against both the DELETE's target and the
// subquery's table and calls it ambiguous at run time — so a condition names
// the alias. Where the DELETE carries the bound itself there is no second
// occurrence of the table and nothing to alias, so a condition names the table.
//
// A condition that got this wrong would not usually fail to parse. It would
// resolve against whatever other table the condition's own subquery names,
// which is a predicate that runs, returns rows, and dooms the wrong ones — so
// the name is asked for here rather than assumed there.
func (g *Generator) PruneQualifier(table string) string {
	mustIdentifier("table name", table)

	if g.boundedWriteForms().has(nativeBound) {
		return table
	}

	return doomedAlias
}

// pruneStatement checks what the two arms both assume — a key, a predicate, and
// an ordering — and hands the rendering to the dialect, which is where the
// shapes part company.
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

	prune.Order = boundedOrder(table, prune.Order)

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
