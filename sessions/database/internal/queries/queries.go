package queries

import (
	"slices"

	"github.com/primandproper/platform-go/v13/database/dialect"
	"github.com/primandproper/platform-go/v13/database/querygen"
)

// SessionsTable is the one table this package owns, at its canonical spelling
// — what the emitted .sql names, and what the backend's prefix rendering starts
// from.
//
// The sessions segment is the schema's own rather than the caller's, so a table
// says which package created it even in a database shared between
// applications; the consumer's namespace goes in front of it. See
// sessions/database/migrations.
const SessionsTable = "sessions"

// TableNames is every table this package owns, which is the one above.
//
// It is a list rather than a bare constant because the querygen registry takes
// one, and because the thing a registry has to survive is a table being added
// without anybody remembering to register it. A slice of one is the shape that
// does not have to change when #320's revocation surface arrives with company.
var TableNames = []string{SessionsTable}

// The columns of a session row, beyond the id every keyed statement here is
// derived from.
//
// Spelled here rather than at the store because both halves read them: the
// corpus below binds them, and the backend's argument structs and scan targets
// name the fields the generated querier derived from them. Two spellings of one
// column is the drift this package exists to prevent.
const (
	// DataColumn holds the encoded payload, and is the one column a write may
	// leave NULL — a session established without a payload reads back as the
	// nil it went in as rather than as a zero value.
	DataColumn = "data"
	// LastSeenAtColumn is the liveness signal the store's policy compares
	// against. It is last_seen_at rather than the convention's
	// last_updated_at because it records a read rather than a mutation — see
	// sessions/database/migrations.
	LastSeenAtColumn = "last_seen_at"
	// ExpiresAtColumn is the deadline the sweeper collects rows past, and it
	// appears in no read predicate. Whether a session is live is decided above
	// this layer from created_at and last_seen_at, so that both backends answer
	// the question identically and clock skew between a writer and a reader
	// cannot hide a live session.
	ExpiresAtColumn = "expires_at"
	// VersionColumn is the payload's shape stamp, a column rather than part of
	// the blob so that it catches a changed payload shape without being
	// unreadable itself.
	VersionColumn = "version"
)

// SweepCutoffArg is the argument the sweep binds its deadline through.
//
// It is named for what it is rather than for the column it is compared against,
// because the two are not the same value: expires_at is a row's own deadline
// and this is the instant the sweep is asking about. See sweepDelete, and
// querygen.BoundTime for why the instant is bound at all rather than read off
// the server.
const SweepCutoffArg = "cutoff"

// Columns is every column, in the order the DDL declares them, the emitted
// INSERT supplies them, and every SELECT here projects them.
//
// created_at is among them and is not database-owned, which is where this table
// parts company with the module's convention: a session's creation time anchors
// an absolute timeout the store computes, so it is the store's to stamp rather
// than the server's. querygen.ForInsert would strip it, which is why the insert
// below is rendered from this list directly.
var Columns = []string{
	querygen.IDColumn,
	DataColumn,
	querygen.CreatedAtColumn,
	LastSeenAtColumn,
	ExpiresAtColumn,
	VersionColumn,
}

// RecordColumns is what the read projects, which is a session record and not a
// session row.
//
// The id is left out because the caller already has it — it is what they asked
// with — and expires_at because nothing above this layer reads it. Narrowing
// the projection is what keeps the column that exists for the sweeper's benefit
// out of the record the store hands back.
var RecordColumns = []string{
	DataColumn,
	querygen.CreatedAtColumn,
	LastSeenAtColumn,
	VersionColumn,
}

// UpdateColumns is what the overwrite assigns.
//
// created_at is deliberately absent. It anchors the absolute timeout, and an
// update — a touch, a payload save, the write half of a renewal — must never
// move it, or the timeout it anchors would stop being absolute. Leaving it out
// of the statement makes that structural rather than a rule somebody has to
// remember, and leaving it out *here* is what makes it structural in every
// dialect at once.
var UpdateColumns = []string{
	DataColumn,
	LastSeenAtColumn,
	ExpiresAtColumn,
	VersionColumn,
}

// NullableColumns names the columns a write may set to NULL, which lives in the
// schema neither this package nor querygen reads.
var NullableColumns = []string{DataColumn}

// sweepColumns is the table's shape as the sweep sees it: every column but the
// id.
//
// querygen derives a statement's id predicate from the column list it is handed,
// so leaving the id out is how a statement says it keys on something else — here
// on a deadline rather than on a row. What it does not decide is which rows come
// back, and a DELETE projects nothing, so this list costs the statement nothing
// else.
var sweepColumns = slices.DeleteFunc(slices.Clone(Columns), func(column string) bool {
	return column == querygen.IDColumn
})

// Render returns the canonical sqlc input for one dialect: every statement the
// backend executes, in the order below, as the bytes the committed .sql beside
// this file holds.
//
// The order is the order a session goes through: created, read, overwritten,
// asked about, removed, and finally collected once its deadline has passed.
func Render(d dialect.Dialect) string {
	g := querygen.For(d)

	// The table registers itself here rather than through StandardCRUD,
	// which is what registers a conventional table and which this one gets
	// none of. A consumer reading the registry back to truncate a database
	// between integration tests has to find this table whether or not the
	// statements over it happen to come from the standard set.
	querygen.RegisterTable(TableNames...)

	return querygen.RenderFile([]*querygen.Query{
		createInsert(g),
		recordRead(g),
		recordUpdate(g),
		existenceCheck(g),
		rowDelete(g),
		sweepDelete(g),
	})
}

// createInsert renders the creation of a session row, ignoring a row that is
// already there.
//
// The conflict clause is what makes ErrIDConflict reportable without parsing a
// driver's error: a duplicate primary key leaves zero rows affected instead of
// raising a dialect-specific SQLSTATE. It is also what keeps a conflict inside
// Rename's transaction from aborting it — Postgres marks a transaction failed
// after a constraint violation, so a caller could not otherwise distinguish
// "that identifier exists" from "your transaction is now unusable".
func createInsert(g *querygen.Generator) *querygen.Query {
	return g.InsertIgnoreQuery("CreateSession", SessionsTable, Columns, NullableColumns,
		querygen.Match{Column: querygen.IDColumn})
}

// recordRead renders the read of one session record by identifier.
func recordRead(g *querygen.Generator) *querygen.Query {
	return g.ReadQuery("GetSession", SessionsTable, Columns,
		querygen.Read{Projection: RecordColumns})
}

// recordUpdate renders the overwrite of an existing session row.
//
// The WHERE clause is the whole guarantee the database backend exists for: a
// row that has been deleted is not recreated, so a request that read a session
// immediately before it was signed out cannot write it back afterwards.
func recordUpdate(g *querygen.Generator) *querygen.Query {
	return g.UpdateQuery("UpdateSession", SessionsTable, Columns, UpdateColumns, NullableColumns)
}

// existenceCheck renders the question the overwrite falls back on.
//
// It is asked only when an UPDATE reported no rows affected, which MySQL also
// reports for an update that matched a row and changed nothing — two saves of
// an identical payload within the same microsecond, say. Without it the second
// would be answered "no such session" and sign a user out over a no-op.
func existenceCheck(g *querygen.Generator) *querygen.Query {
	return g.ExistsQuery("SessionExists", SessionsTable, Columns)
}

// rowDelete renders the removal of one session row.
//
// A hard delete rather than a stamp: this table carries no archived_at, because
// a swept table sized by live sessions is the whole point of the sweeper and a
// soft delete would either do nothing or defeat it.
func rowDelete(g *querygen.Generator) *querygen.Query {
	return g.DeleteQuery("DeleteSession", SessionsTable, Columns)
}

// sweepDelete renders the removal of every row whose deadline has passed.
//
// The deadline is compared against a bound instant rather than the server's
// clock, which is querygen.BoundTime and is the one decision in this file worth
// stopping at. expires_at is written by the backend as now-plus-a-TTL from the
// clock it was constructed with — the interface hands it a duration, not a
// deadline — so the server's CURRENT_TIMESTAMP is a second clock, and under a
// test clock that only moves when a test moves it the two are years apart.
// Binding the same clock's reading keeps both sides of the comparison inside
// one clock, which is what the server-clock comparand asks for rather than an
// exception to it.
func sweepDelete(g *querygen.Generator) *querygen.Query {
	return g.DeleteQuery("SweepSessions", SessionsTable, sweepColumns, querygen.Match{
		Column:  ExpiresAtColumn,
		Arg:     SweepCutoffArg,
		Against: querygen.BoundTime,
	})
}

// FileName is the file one dialect's rendered queries are committed to.
//
// The _generated suffix is in the path rather than only in the header comment,
// because a path is what a reviewer sees in a diff, what CI's glob selects, and
// what a reader scanning this directory reads first — and these are the files
// whose answer to "this line is wrong" is to edit something else.
func FileName(d dialect.Dialect) string {
	return string(d) + "_generated.sql"
}
