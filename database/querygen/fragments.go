package querygen

import (
	"fmt"
	"strings"

	"github.com/primandproper/platform-go/v10/filtering"
)

// JoinStatement is one join in a filtered count's FROM clause: the table being
// joined in, the column on it, and the already-present table and column it is
// matched against.
type JoinStatement struct {
	// JoinTarget is the table being joined in.
	JoinTarget string
	// TargetColumn is the column on JoinTarget the join matches.
	TargetColumn string
	// OnTable and OnColumn name the side already in the query.
	OnTable  string
	OnColumn string
}

// String renders the join clause.
func (j JoinStatement) String() string {
	return fmt.Sprintf("JOIN %s ON %s.%s=%s.%s", j.JoinTarget, j.OnTable, j.OnColumn, j.JoinTarget, j.TargetColumn)
}

// ILIKECondition renders a case-insensitive substring match against a bound
// argument, for a search query's own WHERE predicate.
//
// The argument is bound and the wildcards are concatenated around it, rather
// than the caller passing '%term%' — a caller assembling the pattern is a caller
// who can forget to escape a literal '%' in a user's search term, which turns a
// search for "50%" into a search for everything.
func ILIKECondition(argument string) string {
	return fmt.Sprintf(`ILIKE '%%' || sqlc.arg(%s)::text || '%%'`, argument)
}

// CursorCondition renders the keyset predicate: rows strictly after the cursor.
//
// An absent cursor coalesces to the empty string rather than being handled by a
// second query, which is what keeps the first page and the fiftieth page the same
// statement. It works because id sorts by creation time and no id is empty.
func CursorCondition(table string) string {
	return fmt.Sprintf("%s > COALESCE(sqlc.narg(%s), '')", Qualify(table, IDColumn), CursorArg)
}

// CursorLimitClause renders the ordering and page size a keyset walk needs.
//
// The ORDER BY is not decoration. A cursor names a position in an order, so a
// paginated query without the matching ORDER BY returns rows in whatever order
// the planner found convenient, and the next page's cursor names a position in an
// order that no longer holds — pages that skip rows and repeat others, with
// nothing reporting an error.
func CursorLimitClause(table string) string {
	return fmt.Sprintf("ORDER BY %s ASC\nLIMIT COALESCE(sqlc.narg(%s), %d)",
		Qualify(table, IDColumn), LimitArg, filtering.DefaultQueryFilterLimit)
}

// CursorPaginationFragment renders the cursor predicate and the ordering
// together, for a query that does its own filtering and only wants the keyset
// half.
//
// The predicate arrives prefixed with AND, because the only place it belongs is
// the tail of a WHERE clause that already has one.
func CursorPaginationFragment(table string) string {
	return fmt.Sprintf("AND %s\n%s", CursorCondition(table), CursorLimitClause(table))
}

// ReindexScanQuery builds the keyset walk a search reindex reads its source
// through.
//
// It returns IDs rather than rows on purpose. A Scanner and a Fetcher both have
// to produce the same document for the same row, and the cheapest way to
// guarantee that is to have one of them call the other: the scan names the next
// page of IDs and the fetch — the same one the change feed uses — turns them into
// documents. Selecting rows here would be a second row-to-document transform, and
// two transforms that are supposed to agree are two transforms that can drift.
//
// The ordering is a byte comparison, COLLATE "C", not the database's default
// collation. search/sync requires ascending byte order because the pruning half
// of a reindex merges this stream against the index's own stream of IDs, and
// Postgres's en_US.UTF-8 sorts case-insensitively and ignores punctuation — a
// different order. Two ordered streams merged under disagreeing orders do not
// fail; they conclude that live documents are absent from the source and delete
// them. The Reindexer verifies the order it is given for the same reason.
func ReindexScanQuery(table string) string {
	return fmt.Sprintf(`SELECT %[1]s
FROM %[2]s
WHERE %[3]s IS NULL
	AND %[1]s COLLATE "C" > sqlc.arg(%[4]s)
ORDER BY %[1]s COLLATE "C"
LIMIT COALESCE(sqlc.narg(%[5]s), %[6]d);`,
		Qualify(table, IDColumn),
		table,
		Qualify(table, ArchivedAtColumn),
		CursorArg,
		LimitArg,
		filtering.DefaultQueryFilterLimit,
	)
}

// FilterConditions renders a filtered list query's WHERE clause: the
// filtering.QueryFilter window over whichever of the convention columns the
// table has, then any conditions the caller adds, then the cursor predicate.
//
// It is the whole clause, not an addendum. A caller that opens its own WHERE
// with archived_at IS NULL and appends this one gets a query where
// include_archived cannot do anything, since the first predicate has already
// excluded every row the flag would admit — and nothing about such a query looks
// wrong. Owning the clause is what keeps that from being expressible.
//
// conditions are rendered verbatim, one per line. They are the caller's SQL:
// this package does not parse them and cannot vet them.
func FilterConditions(table string, columns []string, conditions ...string) string {
	return joinPredicates(append(filterPredicates(table, columns, conditions...), CursorCondition(table)), "\t")
}

// FilterCountSelect renders the scalar subquery counting the rows the same
// filter matches, aliased filtered_count.
//
// It is a subquery in the SELECT list rather than a second round trip because
// filtering.QueryFilteredResult wants the page and its counts together, and a
// count issued separately counts a table that has moved on since the page was
// read.
//
// The cursor predicate is deliberately absent: filtered_count answers "how many
// rows match this filter", which does not change as the caller walks through
// them. Including it would count the rows remaining after the cursor, and a total
// that shrinks with every page is a progress bar that never fills.
//
// Because the count rides on the rows, a page with no rows carries no count. A
// caller reporting counts for an empty page has to supply the zero itself, which
// is what filtering.NewQueryFilteredResult taking them as arguments allows.
func FilterCountSelect(table string, columns, joins []string, conditions ...string) string {
	return countSelect("filtered_count", table, joins, filterPredicates(table, columns, conditions...))
}

// TotalCountSelect renders the scalar subquery counting the rows in scope
// regardless of the filter window, aliased total_count.
//
// It applies the same archived handling as the filter — not an unconditional
// archived_at IS NULL — so that filtered_count can never exceed total_count. A
// pair of counts where the subset is larger than the set is the kind of number
// that gets noticed a week later by whoever is reconciling them.
func TotalCountSelect(table string, columns, joins []string, conditions ...string) string {
	var predicates []string

	if has(columns, ArchivedAtColumn) {
		predicates = append(predicates, archivedPredicate(table))
	}

	predicates = append(predicates, conditions...)

	return countSelect("total_count", table, joins, predicates)
}

// countSelect renders one of the two counting subqueries. Both are the same
// statement with a different predicate set and a different alias, and the
// indentation they sit at inside a SELECT list is the part most easily got wrong
// twice.
func countSelect(alias, table string, joins, predicates []string) string {
	lines := []string{
		"(",
		fmt.Sprintf("\t\tSELECT COUNT(%s)", Qualify(table, IDColumn)),
		"\t\tFROM " + table,
	}

	for _, join := range joins {
		lines = append(lines, "\t\t"+join)
	}

	// A WHERE with nothing after it is a syntax error, and a table with none of
	// the convention columns and no caller conditions produces exactly that.
	if len(predicates) > 0 {
		lines = append(lines, "\t\tWHERE "+joinPredicates(predicates, "\t\t\t"))
	}

	return strings.Join(append(lines, fmt.Sprintf("\t) AS %s", alias)), "\n")
}

// filterPredicates is the filter window as individual predicates, in the order
// they are rendered. Both the list query and the count that accompanies it are
// built from this one slice, so the page and the number describing it cannot
// come to disagree about what was filtered.
//
// The cursor is not among them. It is the one part of a QueryFilter that says
// where the caller is rather than what they asked for, so FilterConditions adds
// it and the counts do not.
func filterPredicates(table string, columns []string, conditions ...string) []string {
	var predicates []string

	if has(columns, CreatedAtColumn) {
		predicates = append(predicates,
			boundPredicate(Qualify(table, CreatedAtColumn), ">", CreatedAfterArg),
			boundPredicate(Qualify(table, CreatedAtColumn), "<", CreatedBeforeArg),
		)
	}

	if has(columns, LastUpdatedAtColumn) {
		predicates = append(predicates,
			nullableBoundPredicate(Qualify(table, LastUpdatedAtColumn), ">", UpdatedAfterArg),
			nullableBoundPredicate(Qualify(table, LastUpdatedAtColumn), "<", UpdatedBeforeArg),
		)
	}

	if has(columns, ArchivedAtColumn) {
		predicates = append(predicates, archivedPredicate(table))
	}

	return append(predicates, conditions...)
}

// boundPredicate renders one end of a time window.
//
// An unset bound coalesces to a timestamp 999 years away rather than the
// predicate being omitted, so that all four bounds are the same statement
// whichever subset of them a caller sent. sqlc generates one method per query,
// and a query whose shape depended on which filters were present would need
// sixteen of them.
func boundPredicate(column, comparison, argument string) string {
	sign := "-"
	if comparison == "<" {
		sign = "+"
	}

	return fmt.Sprintf("%s %s COALESCE(sqlc.narg(%s), (SELECT %s %s '999 years'::INTERVAL))",
		column, comparison, argument, NowExpression, sign)
}

// nullableBoundPredicate is boundPredicate for a column that may be NULL, which
// last_updated_at is until the row is first updated.
//
// The NULL arm is not optional. A NULL compares as neither greater nor less than
// anything, so without it an updated_after filter silently excludes every row
// nobody has edited yet — which, on a young table, is nearly all of them.
func nullableBoundPredicate(column, comparison, argument string) string {
	return fmt.Sprintf("(\n\t%s IS NULL\n\tOR %s\n)", column, boundPredicate(column, comparison, argument))
}

// archivedPredicate renders the soft-delete toggle: archived rows are excluded
// unless the caller asked for them.
//
// The flag admits rows; it does not gate the exclusion. Written the other way
// round — NOT COALESCE(flag, false) OR archived_at IS NULL — the predicate is
// true for every row whenever the flag is false, which is to say almost always,
// and the filter admits everything. Nothing about that reads as wrong, and a
// query with a redundant archived_at IS NULL somewhere else in its WHERE behaves
// correctly right up until someone removes the redundancy.
//
// The cast is load-bearing. sqlc.narg types the argument from its use, and
// COALESCE over an untyped NULL leaves Postgres to guess; ::boolean is what makes
// the generated Go field a *bool rather than an interface{} the caller has to
// convince.
func archivedPredicate(table string) string {
	return fmt.Sprintf("(COALESCE(sqlc.narg(%s), false)::boolean OR %s IS NULL)",
		IncludeArchivedArg, Qualify(table, ArchivedAtColumn))
}

// joinPredicates renders predicates as a WHERE clause body at the given indent:
// the first bare, the rest prefixed with AND, each on its own line.
//
// Multi-line predicates are re-indented to match, which is why predicates are
// built with a single level of internal indentation and no knowledge of where
// they will end up — the same predicate appears in a list query's WHERE and,
// two levels deeper, inside the count subquery beside it.
func joinPredicates(predicates []string, indent string) string {
	rendered := make([]string, 0, len(predicates))

	for i, predicate := range predicates {
		predicate = strings.ReplaceAll(predicate, "\n", "\n"+indent)
		if i > 0 {
			predicate = "AND " + predicate
		}

		rendered = append(rendered, predicate)
	}

	return strings.Join(rendered, "\n"+indent)
}
