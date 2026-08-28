package querygen

import (
	"fmt"
)

// DeleteQuery renders the hard delete of the rows one key names: the row gone
// rather than stamped, which is what a right-to-be-forgotten erasure means and
// what a set of child rows being rewritten wholesale needs.
//
// It is the standard single-row machinery with a different verb. The key is the
// column list and the matches, exactly as it is for the get, the update and the
// archive: the id predicate is rendered when the column list has an id, each
// [Match] adds its own equality, and a statement with neither is
// [ErrUnaddressableRow] rather than a DELETE whose WHERE clause is empty and
// whose effect is a truncate.
//
// What it does not render is the archived predicate, and its absence is the one
// thing that distinguishes this from every other statement built on that key. A
// hard delete of an archived row is still a delete — an erasure runs against a
// subject who was archived first, and a child row is cleared whether or not its
// parent has been — so a predicate excluding archived rows here would make the
// erasure the one statement that cannot reach the rows it exists for.
//
// It is annotated :execrows, like the archive, because the count is the answer:
// a caller learns from it whether the row was there, and an erasure reports how
// much it destroyed.
//
// The key is not required to name one row. Clearing every role a membership
// holds is one statement keyed on the membership, and the count is how many
// grants went — so this is "the rows this key names" rather than "the row",
// which is the other half of what separates it from the archive.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) DeleteQuery(name, table string, columns []string, extra ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content:    deleteStatement(table, columns, extra...),
	}
}

// deleteStatement renders the DELETE and the key it addresses rows by.
//
// The predicates are unqualified, as the UPDATE statements' are. A single-table
// DELETE accepts a qualified column on all three servers, but there is nothing
// for the qualifier to disambiguate and the two write verbs spelling their
// WHERE clauses differently would be a difference a reader has to account for.
func deleteStatement(table string, columns []string, extra ...Match) string {
	return fmt.Sprintf("DELETE FROM %s\nWHERE %s;",
		table,
		joinPredicates(keyPredicates(table, columns, "", false, extra), "\t"),
	)
}
