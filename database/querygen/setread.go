package querygen

import (
	"fmt"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v14/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
)

// ErrMissingSetColumn indicates a batched read whose [SetKey] names no column.
//
// What it would render has nothing on the left of the comparison — `table. IN
// (...)` — which is a syntax error on every dialect. It is a programming error
// rather than a caller's, since nothing on a request path decides what a
// statement keys on, so it panics like the rest of this package's misuse.
var ErrMissingSetColumn = platformerrors.New("batched read names no column to key on")

// SetKey is the set-membership predicate a batched read is keyed on: one
// column, matched against a whole set of values bound as a single argument.
//
// It is its own type rather than a flag on [Match] because the two are not the
// same predicate wearing different operators. A Match binds one value and can
// appear anywhere in a statement; a set binds a list whose length is not known
// until the call, which is a bound array on Postgres and a placeholder
// expansion on the other two — and that difference decides where in the
// statement it may sit. See [Generator.SetReadQuery].
type SetKey struct {
	// Column is the column matched against the bound set. It is interpolated,
	// so it is restricted rather than escaped — see dialect.ValidIdentifier.
	Column string
	// Arg names the argument the set binds through, defaulting to [IDsArg].
	//
	// A read whose set is not of ids says so — a batch of usernames, a batch
	// of email addresses — and the name is what a caller's params struct spells
	// it as. There is one set per statement, so the default collides with
	// nothing; the name is for the reader rather than for the compiler.
	Arg string
}

// argument returns the name this key binds through: Arg where the caller gave
// one, and the conventional ids otherwise.
func (k SetKey) argument() string {
	if k.Arg != "" {
		return k.Arg
	}

	return IDsArg
}

// ErrPositionalSetInList indicates a filtered list narrowed by a bound set, on
// a dialect with no array type.
//
// A list carries every predicate three times — once in the WHERE and once in
// each of the two count subqueries beside it — and on MySQL and SQLite a bound
// set is a sqlc.slice expansion rather than one argument. Three expansions of
// one set in one statement is a shape neither sqlc's own generated Go nor
// sqlc-gen-unison's renders: each substitutes the first marker it finds and
// leaves the other two standing, so the statement reaches the server with a
// placeholder count that no argument list matches.
//
// So the shape is Postgres's, where the set is a single bound array and the
// three references are three readings of one named argument. That is a fact
// about the other two engines rather than a decision taken here, and it is
// raised rather than worked around because both workarounds are worse: a
// consumer whose list silently stopped narrowing by state would be a consumer
// listing everything, and a set materialized into a temporary table would be a
// second statement this package has no way to make the caller run.
//
// A three-dialect consumer wanting this read has a portable statement available
// and it is a different one: [Generator.ListQueries] with a [Match] per value,
// for a set whose membership is fixed, or [Generator.SetReadQuery] where the
// page is not what is wanted.
//
// It is a programming error rather than a caller's — a generator binary names
// its dialect and its shapes as literals — so it panics like the rest of this
// package's misuse.
var ErrPositionalSetInList = platformerrors.New("a filtered list cannot bind a set on this dialect")

// SetListQueries renders both directions of a paged list narrowed by a bound
// set: the rows whose keyed column holds any of the values the caller binds,
// under the same filter window, cursor and pair of counts every other list here
// carries.
//
// It is the read behind "show me this owner's failed and cancelled operations":
// a filter over a closed domain, where what the caller has is a set of values
// rather than one. Expressed as [Match] values it would be one statement per
// subset — eight of them for three optional narrowings, sixteen once each is
// emitted in both directions — and a store choosing between sixteen generated
// row types converts rows to its own type sixteen times.
//
// The set is not optional and the empty set matches nothing, which is the same
// contract [Generator.SetReadQuery] carries and the same reason: the arity
// belongs to the values. A caller whose filter is "any of them" binds the whole
// domain rather than binding nothing — which is expressible precisely because
// the domains this shape suits are closed ones — and a caller whose domain is
// not closed wants [OptionalNarrowing] on a single value instead.
//
// # Postgres only
//
// The set is bound three times in one statement, and only an array-typed
// argument can be. See [ErrPositionalSetInList], which is what this panics
// with elsewhere.
//
// Both names must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) SetListQueries(name, table string, columns []string, key SetKey, matches ...Match) []*Query {
	if g.dialect != dialect.Postgres {
		panic(platformerrors.Wrapf(ErrPositionalSetInList, "querygen: dialect %q, table %q", g.dialect, table))
	}

	mustIdentifier("table name", table)

	if key.Column == "" {
		panic(platformerrors.Wrapf(ErrMissingSetColumn, "querygen: table %q", table))
	}

	mustIdentifier("set column", key.Column)
	mustIdentifier("set argument", key.argument())

	return []*Query{
		{
			Annotation: QueryAnnotation{Name: name, Type: ManyType},
			Content:    g.listStatement(table, columns, "", nil, &key, Ascending, matches...),
		},
		{
			Annotation: QueryAnnotation{Name: DescendingName(name), Type: ManyType},
			Content:    g.listStatement(table, columns, "", nil, &key, Descending, matches...),
		},
	}
}

// SetReadQuery renders the read a batched consumer needs: every row whose keyed
// column is in a bound set, ordered by that column.
//
// It is the shape every N+1 read collapses into. A roster page of thirty
// members whose roles are fetched inside the loop that converts rows is thirty
// round trips returning two rows each; the same page reading all thirty
// members' roles through one of these is one. What the caller does with what
// comes back is group it by the keyed column, which is why the ordering is the
// key's rather than the id's — a consumer walking the rows in order sees each
// key's rows together, and [Read.Order] breaks the tie inside one key's group.
//
// # The empty batch is the caller's to answer
//
// A batch of nothing has no statement here. `IN ()` is a syntax error on MySQL
// and SQLite, so there is no text to emit for a zero-length set; what happens
// instead is a convention of whatever generates the Go, and both sqlc and
// sqlc-gen-unison substitute a NULL that matches no row. So an empty batch is
// not a failure — it is a round trip whose answer was known before it was sent,
// on a path that is already there to save round trips.
//
// The contract, then, is that the caller answers it: no keys, no query, no
// rows. It belongs in the caller because the arity does — this package emits
// text, and the length of a set is not a fact about text.
//
// # The set binds last
//
// The set predicate is rendered after every [Match], and that is a requirement
// rather than a layout choice. On the dialects with no array type the set is a
// sqlc.slice expansion — one placeholder per element, each a bare `?` — and
// SQLite numbers a bare marker one past the highest index it has seen, so an
// argument bound after an expansion collides with an element of the set,
// matches nothing, and reports no error. Rendering the set last is what keeps
// the shared argument order the same on all three engines.
//
// # What the column list decides
//
// columns is the table's shape, exactly as it is for the single-row reads: the
// archived predicate is rendered when the list carries archived_at and not
// otherwise, and read.Projection is what the SELECT lists. A hydration read —
// "who created each of these rows" — is a read that wants the archived ones
// too, and it says so by handing over a column list without archived_at in it,
// the same idiom a read keyed on something other than the id uses to leave the
// id predicate off.
//
// The keyed column is not required to be in that list and is not required to be
// projected, though a consumer grouping the rows by it will want it back.
//
// # The key is text
//
// The bound set is a set of text values on every dialect: Postgres casts the
// argument to text[], which is the array type its ANY() reads. That is this
// module's key convention rather than a limitation discovered here — ids are
// xids and natural keys are strings — but a set over an integer column is a
// statement this package does not render.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
//
// It panics rather than returning an error, in the manner of the rest of this
// package: its arguments are string literals in a generator binary. The panic
// value is an error wrapping dialect.ErrInvalidIdentifier or
// [ErrMissingSetColumn].
func (g *Generator) SetReadQuery(name, table string, columns []string, read Read, key SetKey, matches ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ManyType},
		Content:    g.setReadStatement(table, columns, read, key, matches),
	}
}

// setReadStatement renders the batched read: the projection, the predicates the
// column list and the matches justify, the set, and the ordering a consumer
// groups by.
func (g *Generator) setReadStatement(table string, columns []string, read Read, key SetKey, matches []Match) string {
	mustIdentifier("table name", table)

	for _, column := range columns {
		mustIdentifier("column name", column)
	}

	if key.Column == "" {
		panic(platformerrors.Wrapf(ErrMissingSetColumn, "querygen: table %q", table))
	}

	mustIdentifier("set column", key.Column)
	mustIdentifier("set argument", key.argument())

	var predicates []string

	if slices.Contains(columns, ArchivedAtColumn) {
		predicates = append(predicates, Qualify(table, ArchivedAtColumn)+" IS NULL")
	}

	predicates = append(predicates, g.matchPredicates(table, true, matches)...)

	// Last, always — see SetReadQuery on what an argument bound after an
	// expanded set collides with.
	predicates = append(predicates, g.setPredicate(Qualify(table, key.Column), key.argument()))

	return fmt.Sprintf("SELECT\n\t%s\nFROM %s\nWHERE %s%s;",
		strings.Join(QualifyAll(table, read.projecting(columns)), ",\n\t"),
		table,
		joinPredicates(predicates, "\t"),
		listOrderClause(table, setReadOrder(key, read)),
	)
}

// setReadOrder is the ordering a batched read comes back in: the keyed column
// first, so a consumer walking the rows sees one key's rows together, then
// whatever [Read.Order] names to settle the order inside a group.
func setReadOrder(key SetKey, read Read) []Order {
	order := []Order{{Column: key.Column}}

	if read.Order != "" {
		order = append(order, Order{Column: read.Order})
	}

	return order
}
