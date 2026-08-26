package queries

import (
	"slices"

	"github.com/primandproper/platform-go/v13/database/querygen"
)

// Table is one identity table's shape.
//
// Columns is the full list, in the order the emitted SELECTs project it, which
// is also the order identity's scan targets are written in. The rest is what a
// column list cannot say — see the package comment.
type Table struct {
	// Name is the canonical, unprefixed table name.
	Name string
	// Singular and Plural name the entity the emitted query names are built
	// from: GetUser rather than GetIdentityUsers.
	Singular string
	Plural   string

	// Columns is every column, in projection order.
	Columns []string
	// Nullable names the columns a write may set to NULL.
	Nullable []string
	// Updatable names the columns a write may assign after the insert: what the
	// standard update sets, and what an upsert's conflict branch carries over.
	// Everything else this table would otherwise let an update assign becomes
	// immutable to it — see Immutable.
	Updatable []string
	// Omitted names the standard queries this table has no caller for.
	Omitted []querygen.StandardQuery
}

// InsertColumns returns the columns the create supplies values for: everything
// but the database-owned ones.
//
// created_at is among those the database owns, which is the whole reason the
// schema gives it a DEFAULT — see identity/migrations. A caller-supplied
// creation time is how a row ends up with one that disagrees with its id, and
// the cursor walk orders by id while the filter window compares created_at.
func (t *Table) InsertColumns() []string {
	return querygen.ForInsert(t.Columns)
}

// Immutable returns the columns the standard update must not assign: everything
// assignable that Updatable does not name, plus the scope.
//
// Derived rather than declared, so that a column added to a table is immutable
// until somebody says otherwise. The other direction — a new column silently
// joining the update set — is the one with a failure mode, and it is the failure
// mode Updatable's doc describes.
func (t *Table) Immutable() []string {
	assignable := querygen.ForUpdate(t.Columns, ScopeColumn)

	immutable := make([]string, 0, len(assignable))
	for _, column := range assignable {
		if !slices.Contains(t.Updatable, column) {
			immutable = append(immutable, column)
		}
	}

	return immutable
}

// UpdateColumns returns the columns the standard update assigns, in projection
// order.
//
// Order matters because both consumers render from it: the store's Bound update
// and the canonical .sql have to assign the same columns in the same places, and
// deriving both from the column list is what makes that true rather than
// remembered.
func (t *Table) UpdateColumns() []string {
	return querygen.ForUpdate(t.Columns, append(t.Immutable(), ScopeColumn)...)
}

// Options renders this table's shape as the options StandardCRUD reads.
func (t *Table) Options() []querygen.Option {
	opts := []querygen.Option{
		querygen.WithEntity(t.Singular, t.Plural),
		querygen.WithOwnership(ScopeColumn),
		querygen.WithNullable(t.Nullable...),
		querygen.WithImmutable(t.Immutable()...),
	}

	if len(t.Omitted) > 0 {
		opts = append(opts, querygen.WithOmitted(t.Omitted...))
	}

	return opts
}
