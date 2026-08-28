package querygen

import (
	"fmt"

	"github.com/primandproper/platform-go/v13/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// ErrUnorderedBoundedStatement indicates a bounded statement that names no
// ordering.
//
// A LIMIT without an ORDER BY takes whichever rows the server produced first,
// which is a set that may differ between two runs of the same statement against
// the same rows. For a read that is a page nobody can walk; for a write it is a
// pass that can go over the oldest row forever while newer ones behind it are
// collected. The ordering is what makes "the next N" mean something, so it is
// required rather than defaulted — this package has no basis for choosing which
// column a caller's pass should drain in.
//
// It is required of [Generator.PruneQuery] as well as of the sweeps, and that
// is a ruling rather than a symmetry. The argument for letting a horizon pass go
// unordered is a good one as far as it goes: every row the pass could have taken
// instead is still past the horizon on the next pass, so nothing is lost by
// taking them in whatever order the plan produced. That is true of the rows and
// false of everything around them. A reaper sharing its table with keyed writers
// takes row locks in whatever order the plan produced, which is how a pass that
// could not deadlock becomes one that does. A pass that drains oldest-first has
// a backlog age somebody can watch; an unordered one has a count that says
// nothing about how far behind it is. And a statement whose rows differ between
// two runs over the same table is one whose test can assert a count and never
// a row. What the caller pays for all three is an ORDER BY over a column the
// horizon predicate already reads, served by the index that predicate wanted
// anyway.
var ErrUnorderedBoundedStatement = platformerrors.New("bounded statement names no ordering")

// doomedAlias is what a self-referencing capped read calls the table it selects
// from.
//
// It is an alias rather than a bare self-reference because SQLite needs one: a
// subquery selecting from the table its DELETE is deleting from leaves every
// column name reachable from two places, and the failure is an ambiguous-column
// error at run time rather than a parse failure a generator would have seen.
// Postgres does not need it and gets it anyway, because one rendering is one
// thing to get right.
const doomedAlias = "doomed"

// sweepAlias names the derived table a materialized bounded write reads its
// keys through. It is not a table in any schema this module ships, and it never
// reaches a caller: nothing binds it and nothing projects it outward.
const sweepAlias = "bounded"

// boundedWriteForm is the set of spellings a dialect accepts for a write that
// must touch no more than a bounded number of rows.
//
// It is one home for a fact two shapes in this package depend on, because the
// alternative is the state this replaced: the same dialect fact written down in
// two renderings, whose comments had drifted far enough apart to contradict each
// other about what MySQL will accept. There are three spellings, and no dialect
// takes all three:
//
//	                          Postgres   MySQL   SQLite
//	selfReferencingSubquery      yes       no      yes
//	materializedSubquery         yes      yes      yes
//	nativeBound                   no      yes       no
//
// So MySQL is not a dialect with no bounded write. It is a dialect with a
// different pair of them, and an arm rendered there is a choice among what it
// accepts rather than the only thing that parses. Where a shape picks, its own
// doc says why.
type boundedWriteForm uint8

const (
	// selfReferencingSubquery is the bound on a read the write compares
	// against: DELETE FROM t WHERE k IN (SELECT k FROM t WHERE … LIMIT n).
	//
	// It is the only bounded write Postgres and SQLite have, since neither
	// parses a LIMIT on the DELETE itself — Postgres not at all, SQLite only in
	// builds compiled with SQLITE_ENABLE_UPDATE_DELETE_LIMIT, which most are
	// not, so that spelling there is a failure waiting for run time rather than
	// one a generator would see.
	//
	// MySQL refuses it — ER_UPDATE_TABLE_USED, error 1093 — and that refusal is
	// what the other two spellings exist to answer.
	selfReferencingSubquery boundedWriteForm = 1 << iota
	// materializedSubquery is that same scan wrapped in a derived table:
	// DELETE FROM t WHERE k IN (SELECT k FROM (SELECT k FROM t … LIMIT n) AS b).
	//
	// It is what MySQL accepts in place of the self-reference: the derived table
	// is materialized before the outer statement writes anything, so the scan is
	// no longer reading the table being written. It names the same rows in the
	// same order as the self-referencing form, which is what makes it that
	// form's translation rather than a second statement to keep in step.
	//
	// The other two accept it as well, which is why it is not recorded as
	// MySQL's alone. Nothing here renders it for them: a materialization buys
	// them nothing, and the point of a per-dialect arm is to spend the grammar
	// each server actually has.
	materializedSubquery
	// nativeBound is ORDER BY and LIMIT on the DELETE or UPDATE itself, which is
	// MySQL's own grammar for a single-table write.
	//
	// It is the cheapest of the three where it exists — no subquery, no
	// materialization, and the predicates unqualified as every other write
	// verb's here are — and the narrowest: it bounds one statement against one
	// table, and has nowhere to put a lock clause or a projection.
	nativeBound
)

// has reports whether a dialect accepting f accepts form.
func (f boundedWriteForm) has(form boundedWriteForm) bool {
	return f&form != 0
}

// boundedWriteForms reports the spellings this generator's dialect accepts.
//
// Its two callers pick differently, and both are right for their shape.
// Generator.boundedDelete takes nativeBound wherever it is offered, because a
// prune's statement is a DELETE and nothing else renders from it, so the
// cheapest accepted spelling is the whole of the decision.
// Generator.sweepKeyPredicate takes materializedSubquery wherever
// selfReferencingSubquery is refused, because a sweep's predicate is one scan
// shared by a read, a delete and an update — and nativeBound, which has no read
// in it, is a spelling the read could not be rendered from.
func (g *Generator) boundedWriteForms() boundedWriteForm {
	if g.dialect == dialect.MySQL {
		return nativeBound | materializedSubquery
	}

	return selfReferencingSubquery | materializedSubquery
}

// boundedLimit renders a cap clause with the argument spelled the way the
// dialect can carry one.
//
// MySQL takes a placeholder after LIMIT and nothing else — not a COALESCE, not
// a named reference — so every bound reaches it as the bare marker, and
// everything downstream records that marker under [LimitArg] regardless. The
// other two take an expression, which is what lets a page size default and a cap
// refuse to.
//
// So what the two bounds this package renders disagree about is the argument,
// and the dialect fact underneath them is not something they disagree about at
// all: one question — how many rows may this statement touch — spelled once per
// server.
func (g *Generator) boundedLimit(argument string) string {
	if g.dialect == dialect.MySQL {
		return "LIMIT " + g.dialect.Placeholder(1)
	}

	return "LIMIT " + argument
}

// capClause renders the cap one prune pass runs under.
//
// It is [Generator.limitClause]'s sibling and differs from it in the one way
// that matters: the argument is required. A page size left unset is a page of
// the conventional size, which is a reasonable thing for a list to decide on its
// caller's behalf; a cap left unset is the unbounded DELETE the whole shape
// exists to make unspellable, and there is no number this package could pick
// that would be right for a table it knows nothing about.
//
// The name is [LimitArg] on the dialects that can carry one, because "how many
// rows may this statement touch" is one question however the statement got
// there, and a second name for it would be a second thing a caller has to know.
func (g *Generator) capClause() string {
	return g.boundedLimit(fmt.Sprintf("sqlc.arg(%s)", LimitArg))
}

// boundedOrder returns the terms a bounded statement walks, refusing one that
// names none. It is the one place [ErrUnorderedBoundedStatement] is raised, so
// the prune and the sweeps cannot come to answer that question differently.
func boundedOrder(table string, order []Order) []Order {
	if len(order) == 0 {
		panic(platformerrors.Wrapf(ErrUnorderedBoundedStatement, "querygen: table %q", table))
	}

	return order
}
