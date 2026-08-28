package querygen

import (
	"fmt"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// ErrUnpredicatedStatement indicates a statement whose WHERE clause would name
// nothing about the rows it acts on.
//
// A sweep with no [Match] reads, updates, or deletes whatever the planner
// reached first, and the LIMIT beside it makes that look deliberate: a bounded
// DELETE over no predicate is a truncate paid for in installments. So it is a
// programming error rather than a statement, in the manner of
// [ErrUnaddressableRow] one shape over — the difference being that a sweep
// addresses a set rather than a row, and what it needs is therefore a filter
// rather than a key.
var ErrUnpredicatedStatement = platformerrors.New("statement selects rows by nothing")

// Sweep is what a bounded scan lists and the order it drains rows in.
//
// It is the shape a background pass over a table has, and it is not the paged
// list's. A list is a caller walking rows they will look at, so it carries the
// filter window, a cursor naming where that caller had got to, and the counts a
// page is rendered with. A sweep is a process collecting the rows that have
// become due — an artifact past its expiry, a confirmation window that lapsed,
// a record past its retention — and none of those has a reader, a position, or
// a total worth computing. What it has instead is an order that says which rows
// are most overdue and a limit that says how much to do in one pass.
type Sweep struct {
	// Order is the columns the scan walks, most significant first. It is
	// required — see [ErrUnorderedBoundedStatement].
	//
	// The convention the sweeps in this module follow is the column the
	// deadline lives in, then the id: the most overdue rows first, and a
	// deterministic tie-break among the rows that came due in the same instant.
	Order []Order
	// Projection is the columns the SELECT lists, in order. Empty projects the
	// column list the statement was rendered from.
	//
	// A sweep that hands its rows to something outside the database — the
	// artifact expiry, which deletes an object before the row may say it is
	// gone — projects the whole row. One whose next step is a statement wants
	// the id and nothing else, and that one is [Generator.SweepDeleteQuery] or
	// [Generator.SweepUpdateQuery] rather than a scan a caller loops over.
	Projection []string
}

// SweepQuery renders the bounded read a background pass runs: the rows a
// predicate names, most due first, no more than a limit of them.
//
// It is the one read here that is neither a get nor a list, and the two reasons
// are the two things a list has that a sweep must not. A list carries the filter
// window, which describes what a caller asked to see; a sweep's predicate
// describes what has become due, and a window over it would let a caller's
// unrelated date range decide which expired artifacts get collected. And a list
// pages by keyset over the id, which is a position a caller holds between round
// trips; a sweep holds no position at all — each pass starts at the most overdue
// row, because the rows it collected last time are no longer due.
//
// The limit is the page-size argument every other bounded statement here binds,
// so a caller's batch size reaches all three dialects the one way — see
// [Generator.limitClause] for the one place that changes a generated signature.
//
// Whether archived rows come back is decided the way it is everywhere else in
// this package: by the column list. A sweep over a table that soft-deletes
// excludes the archived rows; one whose column list omits archived_at collects
// them too.
//
// A sweep with no [Match] is [ErrUnpredicatedStatement], and one whose [Sweep]
// names no ordering is [ErrUnorderedBoundedStatement].
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) SweepQuery(name, table string, columns []string, sweep Sweep, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ManyType},
		Content:    g.sweepStatement(table, columns, sweep.Projection, sweep.Order, matches),
	}
}

// SweepDeleteQuery renders the bounded hard delete: the rows a predicate names,
// oldest first, no more than a limit of them, gone in one statement.
//
// It is the retention pass, and it is one statement rather than a scan followed
// by deletes for the reason every guarded write in this module is one statement:
// the predicate that decides which rows go is evaluated by the server at the
// moment they go. A scan whose ids are deleted afterwards decides on rows read
// earlier, and what changes in between is precisely what the predicate was
// asking about.
//
// The rows are named through a subquery rather than by a LIMIT on the DELETE
// itself. Two of the three dialects have no such clause; the third, MySQL, does,
// and this shape declines it — see boundedWriteForm for what each server
// accepts. What the subquery is, is [Generator.SweepQuery]'s statement
// projecting the id — the same predicates, the same ordering, the same limit
// clause — so the rows this deletes are the rows that read would have returned,
// and that identity is worth more here than one dialect's cheaper grammar.
//
// It carries no archived predicate of its own, exactly as
// [Generator.DeleteQuery] carries none: an erasure runs against a subject who
// was archived first. What the inner scan does with archived_at is still the
// column list's decision, so a caller that means "the live ones" says so by
// handing over a column list that has the column in it.
//
// It is annotated :execrows because the count is the answer: a sweep reports how
// much it collected, and a pass that came back full is a pass that is not
// keeping up.
//
// # This or the prune
//
// [Generator.PruneQuery] is the other bounded delete here, and the two are not
// interchangeable. This one addresses rows by id, respects archived_at wherever
// the column list carries it, and renders the same scan a read and an update
// also render from. The prune addresses rows by any key — an id or every column
// of a natural key — never excludes an archived row, and renders MySQL's native
// bound and a Postgres lock clause that this shape has nowhere to put.
//
// So: a pass over a soft-deleting table whose rows a caller could also be
// listing takes this one; a retention pass over an append-only table, or one
// keyed on something that is not an id, takes the prune. The package comment's
// "Choosing between the prune and the sweep" works through the reapers this
// module already has.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) SweepDeleteQuery(name, table string, columns []string, order []Order, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content: fmt.Sprintf("DELETE FROM %s\nWHERE %s;",
			table,
			g.sweepKeyPredicate(table, columns, order, matches),
		),
	}
}

// SweepUpdateQuery renders the bounded stamp: the same set of rows
// [Generator.SweepDeleteQuery] would remove, assigned instead.
//
// It is the sweep whose subject has nothing outside the database to clean up —
// a confirmation window that lapsed touches no bucket and no domain — so the
// whole pass is one statement and the count of what moved is what it returns.
// The sweep that does have something outside is [Generator.SweepQuery]: the
// object goes first, and the row is stamped afterwards, one at a time, because a
// bulk write there would leave every artifact in the bucket with nothing left
// pointing at it.
//
// updateColumns is what this statement assigns, exactly as it is for
// [Generator.UpdateQuery], and last_updated_at stamps by convention where the
// column list carries it. The predicates are the inner scan's rather than the
// UPDATE's own, so the rows assigned are the rows the same predicate named
// when the server evaluated it.
//
// It is annotated :execrows because the count is the answer, in the sense the
// bounded delete's is: how many rows this pass collected.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) SweepUpdateQuery(
	name, table string,
	columns, updateColumns, nullable []string,
	order []Order,
	matches ...Match,
) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content: fmt.Sprintf("UPDATE %s SET\n\t%s\nWHERE %s;",
			table,
			strings.Join(g.assignments(columns, updateColumns, nullable), ",\n\t"),
			g.sweepKeyPredicate(table, columns, order, matches),
		),
	}
}

// assignments renders an UPDATE's SET list: one binding per assigned column,
// then the conventional stamp where the table carries it.
//
// It is shared with updateStatement so that a bounded write and a keyed one
// cannot come to disagree about which columns stamp last_updated_at or how a
// nullable one binds.
func (g *Generator) assignments(columns, updateColumns, nullable []string) []string {
	rendered := make([]string, 0, len(updateColumns)+1)
	for _, column := range updateColumns {
		rendered = append(rendered, fmt.Sprintf("%s = %s", column, binding(column, nullable)))
	}

	if slices.Contains(columns, LastUpdatedAtColumn) {
		rendered = append(rendered, fmt.Sprintf("%s = %s", LastUpdatedAtColumn, g.storedNow()))
	}

	return rendered
}

// sweepStatement renders the bounded scan: a projection, the predicates the
// column list and the matches justify, the ordering, and the limit.
//
// The predicates are the batched read's rather than the single-row statements':
// the archived clause where the column list carries archived_at, then one per
// match, and no id predicate — a sweep addresses a set, so a statement keyed on
// the row's own id would be a sweep of exactly one row.
//
// All three sweeps render through here, which is what puts the ordering check on
// the writes as well as on the read: a bounded DELETE that names no order is the
// same non-deterministic set as a bounded SELECT that names none, and the write
// is the one nobody can inspect afterwards.
func (g *Generator) sweepStatement(table string, columns, projection []string, order []Order, matches []Match) string {
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	if len(matches) == 0 {
		panic(platformerrors.Wrapf(ErrUnpredicatedStatement, "querygen: table %q", table))
	}

	if len(projection) == 0 {
		projection = columns
	}

	order = boundedOrder(table, order)

	return fmt.Sprintf("SELECT\n\t%s\nFROM %s\nWHERE %s%s\n%s",
		strings.Join(QualifyAll(table, projection), ",\n\t"),
		table,
		joinPredicates(g.sweepPredicates(table, columns, matches), "\t"),
		listOrderClause(table, order),
		g.limitClause(),
	)
}

// sweepPredicates is what a bounded statement filters on: the archived clause
// where the column list carries the column, then one predicate per match.
func (g *Generator) sweepPredicates(table string, columns []string, matches []Match) []string {
	var predicates []string

	if slices.Contains(columns, ArchivedAtColumn) {
		predicates = append(predicates, Qualify(table, ArchivedAtColumn)+" IS NULL")
	}

	return append(predicates, g.matchPredicates(table, true, matches)...)
}

// sweepKeyPredicate renders the predicate a bounded write addresses its rows
// through: the id in the set the inner scan names.
//
// The outer column is qualified, and that is a requirement rather than a
// flourish — SQLite resolves a bare `id` here against both the statement's
// target and the subquery's table and calls it ambiguous, which is a compile
// error rather than a wrong answer, but only on the one dialect.
//
// MySQL is the shape that differs, and which spelling it gets is not a choice
// made here: the accepted forms are boundedWriteForm's, and this shape takes
// materializedSubquery on the dialect that refuses selfReferencingSubquery
// because its predicate is one scan a read, a delete and an update all render
// from — the third spelling, nativeBound, has no scan in it for the read to be.
// Both forms name the same rows in the same order, and neither is reachable
// from the other's dialect: the [Generator] carries the dialect, so a Postgres
// statement cannot acquire the wrapper or a MySQL one lose it.
func (g *Generator) sweepKeyPredicate(table string, columns []string, order []Order, matches []Match) string {
	if !slices.Contains(columns, IDColumn) {
		panic(platformerrors.Wrapf(ErrMissingIDColumn, "querygen: table %q", table))
	}

	scan := g.sweepStatement(table, columns, []string{IDColumn}, order, matches)

	if !g.boundedWriteForms().has(selfReferencingSubquery) {
		return fmt.Sprintf("%s IN (\n\tSELECT %s\n\tFROM (\n%s\n\t) AS %s\n)",
			Qualify(table, IDColumn),
			Qualify(sweepAlias, IDColumn),
			indentLines(scan, "\t\t"),
			sweepAlias,
		)
	}

	return fmt.Sprintf("%s IN (\n%s\n)", Qualify(table, IDColumn), indentLines(scan, "\t"))
}

// indentLines prefixes every line of a block, so a statement nested inside
// another reads at the depth it sits at.
func indentLines(block, indent string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}

	return strings.Join(lines, "\n")
}
