package querygen

import (
	"fmt"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// CountQuery renders the read that answers how many rows a predicate names, and
// nothing about what is in them.
//
// It is the third of the reads that are not a page of rows, beside
// [Generator.ExistsQuery] and [Generator.SweepQuery], and it is the one a gauge
// wants: the number of requests still owed past their deadline, of jobs still
// waiting, of rows a retention pass has left to collect. Every one of those is a
// number somebody watches over time rather than a page somebody reads, and
// answering it by draining the rows and counting them in Go makes the cost of
// the measurement grow with the thing being measured — which is exactly when a
// gauge is most wanted and least affordable.
//
// It is not the count a list carries. Those two are scalar subqueries riding on
// the page, so the number and the rows describing it come from one snapshot of
// the table — see [Generator.FilterCountSelect]. This one has no page to ride
// on, and asking it is the whole round trip.
//
// The predicates are the sweep's rather than the single-row statements': the
// archived clause where the column list carries archived_at, then one per match,
// and no id predicate at all. That last one is not derived from the column list
// the way it is for a get — a count keyed on the row's own id answers one or
// zero, which is [Generator.ExistsQuery] with more steps, so the shape declines
// it rather than leaving a caller to decline it by handing over a shorter list.
//
// A count over no [Match] at all is [ErrUnpredicatedStatement] rather than a
// count of the table. The unpredicated form is a number about every row a
// database holds for everybody, which is the one number a tenancy-scoped schema
// has no caller for — and a statement that omits the scope column is precisely
// the statement this module's read rule exists to keep unspellable.
//
// It counts rows rather than a column, because that is the question: COUNT over
// a nullable column answers how many of them are set, and a projection chosen
// here would decide that on the caller's behalf.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) CountQuery(name, table string, columns []string, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: OneType},
		Content:    g.countStatement(table, columns, matches),
	}
}

// countStatement renders the count and the predicates it counts under.
func (g *Generator) countStatement(table string, columns []string, matches []Match) string {
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	if len(matches) == 0 {
		panic(platformerrors.Wrapf(ErrUnpredicatedStatement, "querygen: table %q", table))
	}

	return fmt.Sprintf("SELECT COUNT(*)\nFROM %s\nWHERE %s;",
		table,
		joinPredicates(g.sweepPredicates(table, columns, matches), "\t"),
	)
}
