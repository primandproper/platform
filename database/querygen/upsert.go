package querygen

import (
	"fmt"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v13/errors"
)

// ErrDegenerateUpsert indicates an upsert that is not one, in any of the three
// ways a caller can ask for that: no conflict target, so nothing decides which
// row a collision found; no inserted columns, so there is no row to write; or
// nothing assigned on collision, so the statement is an INSERT that fails on
// the second call rather than a write that converges.
//
// Each is a programming error rather than a caller's — nothing on a request
// path decides which columns a statement writes — so it panics like the rest of
// this package's misuse. The wrapped message says which of the three it was.
var ErrDegenerateUpsert = platformerrors.New("upsert would not converge")

// UpsertQuery renders the write that inserts a row or, when a row for the same
// key already exists, brings that row up to date — under one name, in the
// dialect this Generator emits.
//
// It is the one statement in this package whose three renderings differ beyond
// their placeholders. Postgres and SQLite name the conflict target and assign
// through the EXCLUDED alias; MySQL names no target and spells the incoming
// value VALUES(column). The divergence is confined to Generator.conflictHeader
// and Generator.insertedValue, and everything else — the INSERT, the column
// order, the assignments, the arguments, the row that comes out — is the same
// on all three. A consumer generating Go from these files gets one signature.
//
// The key is the conflict target, given as [Match] values, and there is no
// second way to say it: a conflict target declared separately from the key is a
// pair of facts that can disagree, and a conflict target that disagrees with the
// key is only ever a bug. On Postgres and SQLite those columns have to be
// exactly the columns of a unique index the table actually has, or the server
// rejects the statement — "there is no unique or exclusion constraint matching
// the ON CONFLICT specification" — which is the good failure, since it happens
// at sqlc's analysis rather than at the first collision.
//
// # What the conflict branch assigns
//
// updateColumns, less any column the key names, plus two the column list
// decides:
//
//   - archived_at is cleared where the table has one, because an upsert onto a
//     soft-deleted row that left it archived would be a write that reports
//     success and leaves the row invisible to every read. Reviving is what an
//     upsert on a soft-deleting table means; the alternative is a silent no-op.
//   - last_updated_at is stamped where the table has one, from the server's
//     clock, exactly as the standard update stamps it.
//
// A key column named in updateColumns is dropped rather than assigned. On
// Postgres and SQLite assigning it would be a no-op — the row was found by
// matching it — but on MySQL the collision may have been on some *other* unique
// key, primary key included, and the assignment would then move the row onto
// the incoming key rather than restate it. Dropping is the reading that is
// right on all three.
//
// created_at is in neither list, because [ForInsert] excludes it and no caller
// supplies it: the row keeps the creation time the database gave it the first
// time, which is what makes a revived row an old relationship rather than a new
// one.
//
// # The arguments
//
// One per inserted column, bound by column name, and none for the conflict
// branch — every assignment there reads a value the INSERT already carried. So
// an upsert takes exactly the arguments the equivalent create takes, and a
// caller that can build the create's params can build these.
//
// name must be unique across the consumer's whole sqlc package, as every
// [QueryAnnotation].Name must.
func (g *Generator) UpsertQuery(name, table string, columns, insertColumns, updateColumns, nullable []string, key ...Match) *Query {
	return &Query{
		Annotation: QueryAnnotation{Name: name, Type: ExecType},
		Content:    g.upsertStatement(table, columns, insertColumns, updateColumns, nullable, key),
	}
}

// upsertStatement renders the INSERT and the conflict branch it carries.
//
// The INSERT half is createStatement — the same function the create comes from
// — with its terminator taken back off, rather than a second rendering of the
// same column list. An upsert and a create that disagreed about which columns a
// caller supplies, or about which of them may be NULL, would be two statements
// writing one table under two conventions, and only one of them is checked by
// whoever reads the create.
func (g *Generator) upsertStatement(table string, columns, insertColumns, updateColumns, nullable []string, key []Match) string {
	if len(key) == 0 {
		panic(platformerrors.Wrapf(ErrDegenerateUpsert, "querygen: table %q names no conflict target", table))
	}

	if len(insertColumns) == 0 {
		panic(platformerrors.Wrapf(ErrDegenerateUpsert, "querygen: table %q inserts no columns", table))
	}

	keyColumns := make([]string, 0, len(key))
	for _, match := range key {
		keyColumns = append(keyColumns, match.Column)
	}

	assignments := make([]string, 0, len(updateColumns)+2)

	for _, column := range updateColumns {
		if slices.Contains(keyColumns, column) {
			continue
		}

		assignments = append(assignments, fmt.Sprintf("%s = %s", column, g.insertedValue(column)))
	}

	if slices.Contains(columns, ArchivedAtColumn) {
		assignments = append(assignments, ArchivedAtColumn+" = NULL")
	}

	if slices.Contains(columns, LastUpdatedAtColumn) {
		assignments = append(assignments, fmt.Sprintf("%s = %s", LastUpdatedAtColumn, g.storedNow()))
	}

	if len(assignments) == 0 {
		panic(platformerrors.Wrapf(ErrDegenerateUpsert, "querygen: table %q assigns nothing on conflict", table))
	}

	return fmt.Sprintf("%s\n%s\n\t%s;",
		strings.TrimSuffix(createStatement(table, insertColumns, nullable), ";"),
		g.conflictHeader(keyColumns),
		strings.Join(assignments, ",\n\t"),
	)
}
