package querygen

import (
	"fmt"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// A closure read is the one statement in this module that is recursive: a set
// of seed rows, everything reachable from them along a mapping table, and a
// read taken across the whole of it.
//
// It is authorization's permission resolution, and it is the shape a policy
// with inheritance has no way to avoid. A role's permissions are its own plus
// its parents' plus theirs, to whatever depth an operator declared, and the
// depth is data rather than something a statement could be written for. Walking
// it in Go is a round trip per level against a table the server can close over
// in one.
//
// What makes it a shape here rather than a statement somebody writes out is
// that two of its properties are the whole of its correctness and neither is
// visible in a diff of the SQL. It terminates on a cycle, and it excludes
// archived rows at every join rather than only at the seed. Both are rendered
// unconditionally below, which is what makes a corpus that has one and not the
// other unrepresentable rather than merely unusual.

// ErrIncompleteClosure indicates a [Closure] describing half a walk: no CTE
// name, an edge missing a table or one of its two columns, no table to read
// from, or nothing to project out of it.
//
// Every one of them renders SQL a server rejects rather than SQL that quietly
// answers the wrong question, but they are refused here anyway: the message
// says which half is missing, where a parse error says a token was unexpected
// eleven lines into a statement nobody wrote by hand.
//
// It is a programming error rather than a caller's — a generator binary names
// its tables and columns as literals — so it panics like the rest of this
// package's misuse.
var ErrIncompleteClosure = platformerrors.New("closure describes half a walk")

// Edge is a mapping table read in one direction: the column matching a row
// already reached, and the column naming the row reached from it.
//
// It carries no column list, so it can carry no predicates, and that is the
// schema's shape rather than an omission. The mapping tables this reads are the
// id-less child tables — two foreign keys and a primary key over the pair, with
// none of the convention triple — because nothing lists, filters or soft-deletes
// an edge on its own. An edge is live exactly when both of its endpoints are,
// and the endpoints are tables the closure does carry column lists for.
type Edge struct {
	// Table is the mapping table.
	Table string
	// From is the column matched against a row already in the working set.
	From string
	// To is the column naming the row the edge leads to.
	To string
}

// must panics unless e is a whole edge, spelled in identifiers this package
// will interpolate.
func (e Edge) must(kind string) {
	if e.Table == "" {
		panic(platformerrors.Wrapf(ErrIncompleteClosure, "querygen: %s edge names no table", kind))
	}

	if e.From == "" || e.To == "" {
		panic(platformerrors.Wrapf(ErrIncompleteClosure, "querygen: %s edge on table %q names no pair of columns", kind, e.Table))
	}

	mustIdentifier(kind+" edge table", e.Table)
	mustIdentifier(kind+" edge column", e.From)
	mustIdentifier(kind+" edge column", e.To)
}

// join renders the pair of joins one edge contributes: the mapping row matched
// against a row already reached, and the row it names.
func (e Edge) join(alias, aliasColumn, target, targetColumn string) []string {
	return []string{
		JoinStatement{JoinTarget: e.Table, TargetColumn: e.From, OnTable: alias, OnColumn: aliasColumn}.String(),
		JoinStatement{JoinTarget: target, TargetColumn: targetColumn, OnTable: e.Table, OnColumn: e.To}.String(),
	}
}

// Closure is a recursive walk over one mapping table and the read taken across
// everything it reached.
//
// It is taken by pointer, as [Junction] is, because it describes a set of joins
// rather than carrying a value. What a nil one means is where the two part
// company: a nil Junction is a list with no join, which is a statement somebody
// wants, and a nil Closure is a recursive read with no recursion in it, which is
// the plain read [Generator.SetReadQuery] already emits. So nil here is
// [ErrIncompleteClosure] rather than a second spelling of a statement that
// exists.
//
// Three tables are involved and each has a different job. The table
// [Generator.ClosureQuery] is called on is the one the walk moves through —
// roles, in the schema this was written for — and both of [Closure.Walk]'s
// columns name rows of it. [Closure.Reach] hangs off the rows the walk
// accumulated, and [Closure.Table] is what it hangs them off: the permissions
// the resolution is actually asking about.
type Closure struct {
	// Alias names the recursive CTE the walk accumulates into. It is required:
	// a common table expression is addressed by name, and there is no name this
	// package could pick that would not eventually collide with a table in the
	// schema it is rendered against.
	Alias string

	// Key is the column of the walked table an edge's endpoints name, and the
	// one column the CTE carries. It defaults to [IDColumn].
	Key string

	// Walk is the mapping table the recursion follows. Both of its columns name
	// rows of the walked table — a parent and a child, a container and what it
	// contains — and From is the end the walk moves away from: an edge whose
	// From matches a row already reached adds the row its To names.
	//
	// Which end is which is the direction of the closure and this package will
	// not guess it. Reversing the pair answers the opposite question, and both
	// questions are ones somebody wants: what a role inherits, and what
	// inherits from it.
	Walk Edge

	// Reach is the mapping table joining a row the walk reached to the rows the
	// statement reads. Its From matches the accumulated rows and its To names a
	// row of Table.
	Reach Edge

	// Table is the table Reach lands in, and Columns is its column list — what
	// the archived predicate on the far side of the walk is derived from, in
	// the manner of every other statement here.
	Table   string
	Columns []string

	// TableKey is the column of Table that Reach.To names, defaulting to
	// [IDColumn].
	TableKey string

	// Projection is what the statement selects out of Table. It is required and
	// it is separate from Columns for the reason [Read.Projection] is: the
	// column list is what predicates are derived from, and a resolution that
	// wants one column back should not be handed six.
	Projection []string
}

// key is the column the CTE carries, defaulting to the row's own id.
func (c *Closure) key() string {
	if c.Key != "" {
		return c.Key
	}

	return IDColumn
}

// tableKey is the column the reached rows are addressed by, defaulting to the
// row's own id.
func (c *Closure) tableKey() string {
	if c.TableKey != "" {
		return c.TableKey
	}

	return IDColumn
}

// must panics unless c describes a whole walk and a whole read.
func (c *Closure) must() {
	if c == nil {
		panic(platformerrors.Wrap(ErrIncompleteClosure, "querygen: closure read describes no closure"))
	}

	if c.Alias == "" {
		panic(platformerrors.Wrap(ErrIncompleteClosure, "querygen: closure names no common table expression"))
	}

	if c.Table == "" {
		panic(platformerrors.Wrap(ErrIncompleteClosure, "querygen: closure names no table to read"))
	}

	if len(c.Projection) == 0 {
		panic(platformerrors.Wrapf(ErrIncompleteClosure, "querygen: closure on table %q projects nothing", c.Table))
	}

	mustIdentifier("closure alias", c.Alias)
	mustIdentifier("closure key", c.key())
	mustIdentifier("closure table", c.Table)
	mustIdentifier("closure table key", c.tableKey())

	for _, column := range c.Columns {
		mustIdentifier("closure column", column)
	}

	for _, column := range c.Projection {
		mustIdentifier("closure projection column", column)
	}

	c.Walk.must("walk")
	c.Reach.must("reach")
}

// ClosureQuery renders the recursive closure read: the rows key selects, plus
// everything reachable from them along [Closure.Walk], read through
// [Closure.Reach] into [Closure.Table].
//
// It answers "what does this principal's roles grant", which is the one
// question in this module whose answer depends on a depth nothing knows in
// advance:
//
//	resolve := querygen.For(dialect.Postgres).ClosureQuery(
//		"ResolvePermissionsForRoles", "authz_roles", roleColumns,
//		&querygen.Closure{
//			Alias:      "role_closure",
//			Walk:       querygen.Edge{Table: "authz_role_hierarchy", From: "child_role_id", To: "parent_role_id"},
//			Reach:      querygen.Edge{Table: "authz_role_permissions", From: "role_id", To: "permission_id"},
//			Table:      "authz_permissions",
//			Columns:    permissionColumns,
//			Projection: []string{"name"},
//		},
//		querygen.SetKey{Column: "name", Arg: "role_names"})
//
// # UNION, never UNION ALL
//
// The recursive term is UNION, which is what makes the statement terminate on a
// hierarchy that contains a cycle: a row already in the working set is not
// added a second time, so the walk runs out of new rows rather than running
// forever. UNION ALL is the faster spelling and it is the one that hangs.
//
// A store that writes these edges rejects cycles before they are written — that
// is where the error message a person can act on belongs — but a table an
// operator edited by hand has no such guard, and the failure this refuses is a
// query that never returns on the path that decides whether a request is
// allowed. The choice is the shape's rather than the caller's, so a corpus
// cannot carry a resolution that has it the other way round.
//
// # Archived rows are excluded at every join
//
// Both column lists render the archived predicate wherever their table appears:
// the seed, the recursive term, and the read on the far side. So archiving a
// role stops the walk at it rather than merely refusing it as a seed, and
// archiving a permission revokes it everywhere on the next resolution without
// touching a mapping row.
//
// Excluding archived rows only at the seed is the mistake this forecloses, and
// it is a comfortable one to make: the statement still looks keyed, still
// returns rows, and still passes a test that archives the role it asks about.
// What it does is keep granting through an archived intermediary.
//
// The mapping tables carry no such predicate and cannot — see [Edge] — which is
// the same fact from the other side: an edge is live exactly when the rows at
// both of its ends are.
//
// # The seed
//
// The seed is a bound set rather than one value, because the question is always
// asked of the roles a principal holds and a principal holds several. matches
// narrow it further, on the walked table's own columns; there is nothing to
// narrow the far side by beyond the archived predicate, because a caller
// filtering the permissions a resolution returns is a caller asking a different
// question.
//
// The set is bound last in the seed's WHERE clause, as every bound set this
// package renders is: an expansion is a run of bare markers on two of the three
// dialects, and an argument numbered after one collides with an element of it.
//
// # The answer
//
// DISTINCT, and ordered by the projection. Two roles granting one permission is
// the ordinary case rather than an anomaly, so the duplicate is the walk's
// arithmetic showing through rather than an answer; and a set whose order is
// whichever the planner found convenient is a set two identical calls can
// return differently. The ORDER BY names exactly the projected columns, which is
// what a SELECT DISTINCT is allowed to order by on all three servers.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
//
// It panics rather than returning an error, in the manner of the rest of this
// package: its arguments are string literals in a generator binary. The panic
// value is an error wrapping dialect.ErrInvalidIdentifier, [ErrIncompleteClosure]
// or [ErrMissingSetColumn].
func (g *Generator) ClosureQuery(name, table string, columns []string, closure *Closure, key SetKey, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ManyType},
		Content:    g.closureStatement(table, columns, closure, key, matches),
	}
}

// closureStatement renders the whole of it: the seed, the recursive term, and
// the read across what they accumulated.
func (g *Generator) closureStatement(table string, columns []string, closure *Closure, key SetKey, matches []Match) string {
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	if key.Column == "" {
		panic(platformerrors.Wrapf(ErrMissingSetColumn, "querygen: table %q", table))
	}

	mustIdentifier("set column", key.Column)
	mustIdentifier("set argument", key.argument())

	closure.must()

	walked := Qualify(table, closure.key())

	return fmt.Sprintf("WITH RECURSIVE %s AS (\n\tSELECT\n\t\t%s\n\tFROM %s\n\tWHERE %s\n\tUNION\n\tSELECT\n\t\t%s\n\t%s%s\n)\nSELECT DISTINCT\n\t%s\n%s\nWHERE %s\nORDER BY %s;",
		closure.Alias,
		walked,
		table,
		joinPredicates(g.seedPredicates(table, columns, key, matches), "\t\t"),
		walked,
		strings.Join(append(
			[]string{"FROM " + closure.Alias},
			closure.Walk.join(closure.Alias, closure.key(), table, closure.key())...,
		), "\n\t"),
		recursivePredicate(table, columns),
		strings.Join(QualifyAll(closure.Table, closure.Projection), ",\n\t"),
		strings.Join(append(
			[]string{"FROM " + closure.Alias},
			closure.Reach.join(closure.Alias, closure.key(), closure.Table, closure.tableKey())...,
		), "\n"),
		joinPredicates(archivedPredicates(closure.Table, closure.Columns), "\t"),
		strings.Join(QualifyAll(closure.Table, closure.Projection), ", "),
	)
}

// seedPredicates is the WHERE clause of the non-recursive term: the walked
// table's own archived predicate, whatever the caller narrowed by, and the set
// of keys the walk starts from.
func (g *Generator) seedPredicates(table string, columns []string, key SetKey, matches []Match) []string {
	predicates := archivedPredicates(table, columns)

	predicates = append(predicates, g.matchPredicates(table, true, matches)...)

	// Last, always — see Generator.SetReadQuery on what an argument bound after
	// an expanded set collides with.
	return append(predicates, g.setPredicate(Qualify(table, key.Column), key.argument()))
}

// recursivePredicate is the recursive term's WHERE clause, which is the archived
// predicate and nothing else — the joins have already said which rows this term
// is about.
//
// A table with no archived_at gets no clause at all rather than an empty WHERE,
// which is a syntax error on every dialect.
func recursivePredicate(table string, columns []string) string {
	predicates := archivedPredicates(table, columns)
	if len(predicates) == 0 {
		return ""
	}

	return "\n\tWHERE " + joinPredicates(predicates, "\t\t")
}

// archivedPredicates is the liveness clause a column list justifies: one
// predicate where the table soft-deletes, and none where it does not.
//
// It returns a slice rather than a string so that a term with no other
// predicate renders no WHERE at all — see recursivePredicate.
func archivedPredicates(table string, columns []string) []string {
	if !slices.Contains(columns, ArchivedAtColumn) {
		return nil
	}

	return []string{Qualify(table, ArchivedAtColumn) + " IS NULL"}
}
