package querygen

import (
	"fmt"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// ErrDegenerateInsert indicates an insert that is not one, in either of the two
// ways a caller can ask for that: no columns, so there is no row to write and
// the statement is a syntax error rather than an empty row; or, for the
// insert-ignore, no conflict target, so nothing says which collision is the one
// being skipped.
//
// Each is a programming error rather than a caller's — nothing on a request
// path decides which columns a statement writes — so it panics like the rest of
// this package's misuse. The wrapped message says which of the two it was.
var ErrDegenerateInsert = platformerrors.New("insert would not write a row")

// InsertQuery renders the write that adds a row, under a name of the caller's
// choosing.
//
// It is [Generator.StandardCRUD]'s create with the id requirement lifted off
// it, which is the whole reason it is here. StandardCRUD needs an id because it
// emits the list, and the list pages by keyset over that column; an INSERT
// needs no such thing — a create is "these columns, these bindings" whatever
// the table keys on. So the child tables keyed on their parent, whose primary
// key is (parent_id, value) and whose whole set StandardCRUD refuses, get their
// create from here instead of from a hand-written statement. A natural-key
// table is the same case one shape earlier: an INSERT keys on nothing, so it is
// the one statement such a table wants unchanged from the standard set while
// every other one it wants keyed on that natural key — without this, its corpus
// would be five statements sqlc checks and a sixth nobody could render.
//
// insertColumns is what the caller supplies — [ForInsert] over the table's
// columns — and nullable names those whose value may be NULL, exactly as they
// mean for the standard create. There is one argument per column, bound by
// column name.
//
// A set of child rows is written one statement per element rather than one
// statement with a VALUES list per call. The multi-row form has no static text:
// its shape is the caller's cardinality, so there is nothing for sqlc to check
// and nothing for this package to emit. The cardinalities that reach it are
// single-digit — the roles one membership holds — and inside the transaction
// the parent's write already opened, so what it costs is a round trip each.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) InsertQuery(name, table string, insertColumns, nullable []string) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecType},
		Content:    createStatement(table, insertColumns, nullable),
	}
}

// InsertIgnoreQuery renders the write that adds a row unless one for the same
// key is already there, in the dialect this Generator emits.
//
// It is a named shape rather than an upsert whose conflict branch assigns
// nothing — [ErrDegenerateUpsert] refuses that, and correctly, since an upsert
// is a write that converges and one assigning nothing is an INSERT that fails
// on the second call. This one does not fail on the second call and does not
// converge either: the row that is already there wins, unchanged, and the count
// is how the caller learns it lost. That is the write a key mint wants — the
// loser of a race between two replicas has generated a key it must throw away,
// because a second live key for one subject is a shred that leaves half the
// ciphertext readable — and it is why the statement is annotated :execrows
// while the plain insert is :exec.
//
// The three renderings differ in shape rather than in an expression, as the
// upsert's do, and confined the same way: [Generator.ignoreSpelling] is the
// whole of it. Postgres takes a trailing ON CONFLICT (…) DO NOTHING; MySQL and
// SQLite take a modifier between the verb and INTO — INSERT IGNORE and INSERT
// OR IGNORE — and name no target at all.
//
// The key is the conflict target, given as [Match] values, and the rule is the
// upsert's rule: those columns have to be exactly the columns of a unique index
// the table actually has, or Postgres rejects the statement at sqlc's analysis
// rather than at the first collision. The same caveat [Generator.conflictHeader]
// carries applies here — MySQL's IGNORE fires on whichever unique key was
// violated, primary key included, rather than on the one named. Nothing follows
// from that here the way it does for the upsert, since this statement assigns
// nothing to the row it found, but a table with a second unique index skips a
// collision on Postgres that it would have raised on.
//
// The matches are the target and not a predicate: an INSERT has no WHERE, so
// nothing binds them and they contribute no arguments. The arguments are one
// per inserted column, as the plain insert's are.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) InsertIgnoreQuery(name, table string, insertColumns, nullable []string, key ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecRowsType},
		Content:    g.insertIgnoreStatement(table, insertColumns, nullable, key),
	}
}

// insertIgnoreStatement renders the INSERT and whichever half of the dialect's
// duplicate-skipping spelling goes outside it.
//
// The INSERT half is insertStatement — the same function the create and the
// upsert's first half come from — so a table's plain insert and its
// insert-ignore cannot come to disagree about which columns a caller supplies
// or about which of them may be NULL.
func (g *Generator) insertIgnoreStatement(table string, insertColumns, nullable []string, key []Match) string {
	if len(key) == 0 {
		panic(platformerrors.Wrapf(ErrDegenerateInsert, "querygen: table %q names no conflict target", table))
	}

	keyColumns := make([]string, 0, len(key))
	for i := range key {
		keyColumns = append(keyColumns, key[i].Column)
	}

	modifier, clause := g.ignoreSpelling(keyColumns)

	statement := insertStatement(modifier, table, insertColumns, nullable)
	if clause == "" {
		return statement
	}

	return fmt.Sprintf("%s\n%s;", strings.TrimSuffix(statement, ";"), clause)
}

// insertStatement renders an INSERT: the modifier the dialect wants between the
// verb and INTO — empty for every insert but the ignoring one — then the
// columns and one binding each.
//
// An INSERT with an empty column list is not a degenerate insert, it is a
// syntax error, so it is ErrDegenerateInsert rather than text a server will
// reject later. StandardCRUD never reaches it: naming the id itself
// database-owned leaves nothing to insert, and it omits the create instead.
func insertStatement(modifier, table string, insertColumns, nullable []string) string {
	if len(insertColumns) == 0 {
		panic(platformerrors.Wrapf(ErrDegenerateInsert, "querygen: table %q inserts no columns", table))
	}

	values := make([]string, 0, len(insertColumns))
	for _, column := range insertColumns {
		values = append(values, binding(column, nullable))
	}

	return fmt.Sprintf("INSERT %sINTO %s (\n\t%s\n) VALUES (\n\t%s\n);",
		modifier,
		table,
		strings.Join(insertColumns, ",\n\t"),
		strings.Join(values, ",\n\t"),
	)
}
