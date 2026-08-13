package querygen

import (
	"fmt"
	"slices"
)

// The columns this module has opinions about. A table is free to hold any others
// it likes; these are the ones whose presence changes what gets emitted, and
// whose names are spelled here rather than in each generator so that a table
// calling its soft-delete column something else is a table this package does not
// claim to serve.
const (
	// IDColumn is the primary key, and also the pagination cursor. Both roles
	// require it to sort by creation time — an xid or a ULID, not a serial and
	// not a UUIDv4 — because a keyset walk over an id that does not sort that
	// way pages in an order nobody asked for.
	IDColumn = "id"
	// CreatedAtColumn carries the row's creation time and bounds the
	// created_after/created_before window.
	CreatedAtColumn = "created_at"
	// LastUpdatedAtColumn is NULL until the row is first updated, which is why
	// every predicate over it admits NULL explicitly.
	LastUpdatedAtColumn = "last_updated_at"
	// ArchivedAtColumn is the soft delete. Rows are archived rather than
	// deleted, so every read filters on it and no write removes a row.
	ArchivedAtColumn = "archived_at"
	// LastIndexedAtColumn records when a row was last written to a search
	// index. Its presence is what marks a table as one search/sync mirrors.
	LastIndexedAtColumn = "last_indexed_at"
	// BelongsToAccountColumn is the conventional owner of a tenant-scoped row.
	// It is a name, not a behavior: scoping queries by it is WithOwnership's
	// job, because whether a table's rows are readable across accounts is a
	// decision about that table and not something to infer from a column.
	BelongsToAccountColumn = "belongs_to_account"
)

// The sqlc argument names the emitted queries bind. They are the SQL-side
// spelling of filtering.QueryFilter — see the package comment for the mapping
// between these, the struct fields, and the URL parameters.
const (
	CursorArg          = "cursor"
	LimitArg           = "result_limit"
	IncludeArchivedArg = "include_archived"
	CreatedAfterArg    = "created_after"
	CreatedBeforeArg   = "created_before"
	UpdatedAfterArg    = "updated_after"
	UpdatedBeforeArg   = "updated_before"
)

// NowExpression is how the emitted SQL asks for the current time.
//
// The server's clock, never the application's. A row's created_at and a
// filter's created_after are compared against each other, so they have to come
// from the same clock; two application instances whose clocks differ by a second
// would otherwise write rows that a window excludes at random.
const NowExpression = "NOW()"

// databaseOwnedColumns are the columns the database fills in and a caller must
// never supply. Three of them are set by the statements this package emits, and
// last_indexed_at is set by whatever writes the search index.
//
// Letting a caller pass created_at is not a small liberty: it is how a row ends
// up with a creation time that disagrees with its id, and the cursor walk orders
// by id while the window filters on created_at.
var databaseOwnedColumns = []string{
	ArchivedAtColumn,
	CreatedAtColumn,
	LastUpdatedAtColumn,
	LastIndexedAtColumn,
}

// ForInsert returns the columns an INSERT takes values for: everything but the
// database-owned ones, and anything else the caller names.
//
// Order is preserved, because an INSERT's column list and its VALUES list are
// positional and have to be rendered from the same slice.
func ForInsert(columns []string, exceptions ...string) []string {
	return without(columns, append(slices.Clone(databaseOwnedColumns), exceptions...)...)
}

// ForUpdate returns the columns an UPDATE assigns: ForInsert's set, less the id.
//
// The id is excluded because an UPDATE keys on it. A SET that assigns the column
// the WHERE matches on is a row that changes its own identity mid-statement,
// which is legal SQL and never what anyone meant.
func ForUpdate(columns []string, exceptions ...string) []string {
	return ForInsert(columns, append(slices.Clone(exceptions), IDColumn)...)
}

// Qualify renders a column as table.column.
func Qualify(table, column string) string {
	return fmt.Sprintf("%s.%s", table, column)
}

// QualifyAll renders every column as table.column, preserving order.
func QualifyAll(table string, columns []string) []string {
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, Qualify(table, column))
	}

	return out
}

// without returns the elements of columns that are not in excluded, in order.
func without(columns []string, excluded ...string) []string {
	out := make([]string, 0, len(columns))

	for _, column := range columns {
		if !slices.Contains(excluded, column) {
			out = append(out, column)
		}
	}

	return out
}
