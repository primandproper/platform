package querygen

import (
	"fmt"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// A junction list is the one read this package emits that spans two tables: a
// page of one table's rows reached through a junction — the row that says a
// user belongs to an account, that a document is tagged, that a subscription
// covers an event type — and, where the caller wants them, the junction's own
// columns beside them.
//
// It is deliberately one join rather than a join builder. Every read of this
// shape in this module is the junction pattern, which is this module's own
// convention rather than a consumer's schema: a table keying two owned entities,
// with facts of its own. A general builder would have to accept an arbitrary
// FROM clause, and there is no caller for one — where the narrow shape can widen
// later at a known cost, the wide one can never narrow.
//
// What it is not is a second rendering of a filtered list. The paged form is
// listStatement with a join spliced into its FROM, so the window, the archived
// toggle, the cursor and the two counts are the same code path a single-table
// list goes through, and a junction list filters exactly as an unkeyed one does
// because there is nothing that could make it not.

// ErrIncompleteJunction indicates a Junction that describes half a join: no
// table, or a table with no pair of columns to match it on.
//
// It is rejected rather than ignored because ignoring it fails quietly. A
// Junction whose Table was left empty still carries its Matches, and dropping
// them silently is a keyed read that lost its key — every row in the table,
// returned without an error, under a query name that says otherwise. A list with
// no join says so by passing no Junction at all.
var ErrIncompleteJunction = platformerrors.New("junction describes half a join")

// Junction is the second table a junction list reads through: how it is joined,
// what it contributes to the WHERE clause, and whether its columns are projected
// beside the listed table's.
//
// A nil *Junction is no join at all. That is what a read of the junction's own
// rows takes — a user's memberships, keyed on the user — and it is why
// [Generator.JunctionListAllQuery] takes one rather than assuming one.
type Junction struct {
	// Table is the table joined in, and it is required — a list with no join
	// passes no Junction rather than an empty one.
	Table string

	// Column is the column on Table the join matches, and OnColumn is the
	// column on the listed table it is matched against. Both are required when
	// Table is set: a join needs two sides, and this package will not guess
	// which column of one table points at the other.
	Column   string
	OnColumn string

	// Prefix is the alias every projected column of Table carries — a Prefix of
	// "user" renders users.id AS user_id — and an empty Prefix projects none of
	// them.
	//
	// It is not decoration and it is not optional for a projection. Two tables
	// following this module's row conventions share most of their column names,
	// so an unaliased two-table projection has two columns called id, two called
	// scope, and two called created_at. What a generator downstream makes of
	// that is its own business — sqlc suffixes the repeats with an ordinal — and
	// the result is a row type whose field names depend on the order the SELECT
	// happened to list its tables in. Naming the prefix is what keeps the row
	// type readable and stable; requiring it for a projection is what makes the
	// unaliased case unrepresentable rather than merely discouraged.
	Prefix string

	// Columns is Table's full column list, in the order a projection should
	// list them.
	//
	// It is the whole list rather than the projected subset, because the
	// predicates the join contributes are derived from it the way every other
	// statement here derives its predicates: a joined table with an archived_at
	// is required to be live, and one without it is not asked to be. A caller
	// that wants the join's predicates and none of its columns supplies the
	// columns and no Prefix.
	Columns []string

	// Matches are equality predicates on Table's columns — the key a junction
	// list is read by when the key lives on the far side of the join, which is
	// what "the accounts this user belongs to" is.
	//
	// Each binds under its own column name, as the listed table's matches do, so
	// a column named on both sides of the join binds one argument to both. Two
	// tables that genuinely need separate values for a shared column name are
	// outside what this shape expresses.
	Matches []Match
}

// joined reports whether j names a table to join. A nil Junction is the absence
// of a join rather than a programming error — see the type comment.
func (j *Junction) joined() bool { return j != nil && j.Table != "" }

// must panics unless j is a complete join, spelled in identifiers this package
// will interpolate. No junction at all is not a failure — it is how a list says
// it reads one table.
func (j *Junction) must() {
	if j == nil {
		return
	}

	if j.Table == "" {
		panic(platformerrors.Wrap(ErrIncompleteJunction, "querygen: junction names no table"))
	}

	if j.Column == "" || j.OnColumn == "" {
		panic(platformerrors.Wrapf(ErrIncompleteJunction, "querygen: junction on table %q names no join columns", j.Table))
	}

	mustIdentifier("junction table", j.Table)
	mustIdentifier("junction column", j.Column)
	mustIdentifier("junction join column", j.OnColumn)

	for _, column := range j.Columns {
		mustIdentifier("junction column", column)
	}

	for _, match := range j.Matches {
		mustIdentifier("junction match column", match.Column)
	}

	if j.Prefix != "" {
		mustIdentifier("junction column prefix", j.Prefix)
	}
}

// joins renders the join clause, as the list both the statement's FROM and the
// count subqueries beside it take.
//
// It goes through JoinStatement rather than rendering the clause here, so the
// join a count reads over is the same text as the join the page reads over. A
// count taken over a different FROM clause than its page is a number describing
// a collection nobody asked about.
func (j *Junction) joins(table string) []string {
	if !j.joined() {
		return nil
	}

	return []string{JoinStatement{
		JoinTarget:   j.Table,
		TargetColumn: j.Column,
		OnTable:      table,
		OnColumn:     j.OnColumn,
	}.String()}
}

// conditions renders what the join contributes to the WHERE clause: that the
// joined row is live, and whatever key it is matched on.
//
// The liveness is unconditional rather than gated on include_archived, which the
// listed table's own archived_at is. The distinction is what each column means
// here: the filter window describes the rows being listed, and the joined row is
// a reference those rows hold. A roster asked for archived memberships wants the
// memberships that ended, not the users who were deleted.
func (j *Junction) conditions() []string {
	if !j.joined() {
		return nil
	}

	var conditions []string

	if slices.Contains(j.Columns, ArchivedAtColumn) {
		conditions = append(conditions, Qualify(j.Table, ArchivedAtColumn)+" IS NULL")
	}

	return append(conditions, matchPredicates(j.Table, true, j.Matches)...)
}

// projection renders the joined table's columns, each aliased with Prefix. An
// empty Prefix projects none of them — see Junction.Prefix.
func (j *Junction) projection() []string {
	if !j.joined() || j.Prefix == "" {
		return nil
	}

	projected := make([]string, 0, len(j.Columns))
	for _, column := range j.Columns {
		projected = append(projected, fmt.Sprintf("%s AS %s_%s", Qualify(j.Table, column), j.Prefix, column))
	}

	return projected
}

// Order is one ORDER BY term of an unpaged list: a column on the listed table,
// and which way it sorts.
//
// The direction is spelled in the emitted SQL either way rather than leaning on
// the server's default, so that reading the statement answers the question the
// reader has.
type Order struct {
	// Column is the column sorted on.
	Column string
	// Descending sorts the column the other way — the flag a default-first
	// ordering puts on the flag column.
	Descending bool
}

// String renders the term as an ORDER BY spells it, unqualified.
func (o Order) String() string {
	if o.Descending {
		return o.Column + " DESC"
	}

	return o.Column + " ASC"
}

// qualified renders the term against the table it sorts, which is the form the
// emitted SQL carries. It goes through String so the direction is spelled in one
// place.
func (o Order) qualified(table string) string {
	return Order{Column: Qualify(table, o.Column), Descending: o.Descending}.String()
}

// orderClause renders an unpaged list's ordering, or nothing when the caller
// named none.
//
// A list with no ordering returns its rows in whatever order the planner found
// convenient, which is a legitimate thing to ask for — a caller draining every
// row and sorting them itself gains nothing from the server doing it first — and
// a quiet surprise for one that assumed otherwise. So it is the caller's to name
// rather than something defaulted here, and the paged form does not accept one
// at all: its order is the cursor's, and a page ordered by anything else is a
// keyset walk whose cursor names a position in an order that no longer holds.
func orderClause(table string, order []Order) string {
	if len(order) == 0 {
		return ""
	}

	terms := make([]string, 0, len(order))
	for _, term := range order {
		mustIdentifier("order column", term.Column)
		terms = append(terms, term.qualified(table))
	}

	return "\nORDER BY " + strings.Join(terms, ", ")
}

// fromClause renders the FROM and whatever the junction joins to it.
func fromClause(table string, joins []string) string {
	return strings.Join(append([]string{"FROM " + table}, joins...), "\n")
}

// JunctionListQuery renders the paged junction list: a page of table's rows
// reached through junction, under the same filter window, archived toggle,
// cursor and pair of counts every other list in this package carries.
//
// It is [Generator.listStatement] with a join spliced into its FROM — the same
// function StandardCRUD's list comes from — so there is one filtered read in
// this package rather than two that could come to disagree about what a filter
// means. The counts carry the join too, which is what keeps filtered_count a
// count of the rows the page is drawn from rather than of the listed table
// entire.
//
// The cursor pages over table's id, so table is the entity being listed and
// junction is what it is reached through. Which of the two is which is the one
// decision a caller has to make, and it is decided by what a page of results is
// a page of: an account's roster is a page of memberships with a user attached,
// where a user's account list is a page of accounts reached through memberships.
//
// The name must be unique across the consumer's whole sqlc package, as every
// QueryAnnotation.Name must.
//
// A nil junction renders no join, which is exactly [Generator.ListQuery]'s
// statement under a name of the caller's choosing. The unpaged form is where a
// nil junction is the ordinary case.
func (g *Generator) JunctionListQuery(name, table string, columns []string, junction *Junction, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ManyType},
		Content:    g.junctionListStatement(table, columns, junction, matches...),
	}
}

// JunctionListAllQuery renders the unpaged junction list: every row the matches
// select, in the order the caller names.
//
// It is the paged form with everything a page implies removed — no filter
// window, no cursor, no LIMIT, and no counts, because a caller reading every row
// counts them by looking at what came back. What survives is the projection, the
// join, the matches and the archived predicate.
//
// Archived rows are excluded outright rather than through include_archived. An
// unpaged list takes no filtering.QueryFilter — that is what unpaged means here
// — so there is no flag to read, and a caller who wants archived rows back wants
// the paged form rather than an argument on this one.
//
// order is the caller's, and may be empty; see orderClause for what an empty one
// means. The terms name columns on table, not on the junction.
func (g *Generator) JunctionListAllQuery(name, table string, columns []string, junction *Junction, order []Order, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ManyType},
		Content:    g.junctionListAllStatement(table, columns, junction, order, matches...),
	}
}

// junctionListStatement is the paged statement, which is listStatement's.
func (g *Generator) junctionListStatement(table string, columns []string, junction *Junction, matches ...Match) string {
	mustJunctionList(table, columns, junction)

	return g.listStatement(table, columns, "", junction, matches...)
}

// junctionListAllStatement is the unpaged statement.
func (g *Generator) junctionListAllStatement(table string, columns []string, junction *Junction, order []Order, matches ...Match) string {
	mustJunctionList(table, columns, junction)

	var predicates []string

	if slices.Contains(columns, ArchivedAtColumn) {
		predicates = append(predicates, Qualify(table, ArchivedAtColumn)+" IS NULL")
	}

	predicates = append(predicates, matchPredicates(table, true, matches)...)
	predicates = append(predicates, junction.conditions()...)

	statement := fmt.Sprintf("SELECT\n\t%s\n%s",
		strings.Join(append(QualifyAll(table, columns), junction.projection()...), ",\n\t"),
		fromClause(table, junction.joins(table)),
	)

	// A WHERE with nothing after it is a syntax error, which a table with no
	// archived_at read without matches through no junction produces exactly.
	// Reading every row of a small table is a query somebody means; a WHERE
	// keyword with no predicate is not.
	if len(predicates) > 0 {
		statement += "\nWHERE " + joinPredicates(predicates, "\t")
	}

	return statement + orderClause(table, order) + ";"
}

// mustJunctionList panics unless every identifier a junction list interpolates
// is one this package will interpolate, in the manner of the rest of this
// package: the arguments are string literals in a generator binary, so every way
// this fails is a typo a build should stop for.
func mustJunctionList(table string, columns []string, junction *Junction) {
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	junction.must()
}
