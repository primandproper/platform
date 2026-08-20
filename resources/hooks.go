package resources

import (
	"context"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/tenancy"
)

// Op names what a write did.
type Op string

const (
	// OpCreated is a row that did not exist before.
	OpCreated Op = "created"
	// OpUpdated is a row whose mutable columns were reassigned.
	OpUpdated Op = "updated"
	// OpArchived is a row that was soft-deleted, whether singly or as part of a
	// cascade.
	OpArchived Op = "archived"
)

// Change describes one write, for the hooks that run inside the transaction that
// performed it.
type Change[T any] struct {
	// Row is the row the change is about, read inside the transaction that
	// performed the write: as stored afterwards for a create or an update, and
	// as it stood before it went for an archive, which is the last moment its
	// columns are readable through a store that filters archived rows out.
	//
	// A cascade reports one Change per row rather than one for the statement,
	// so this is populated there too — that is what lets a hook record what a
	// cascade actually touched instead of that one happened.
	Row *T
	// Resource is the definition's Name, and Table its table, so one hook
	// registered across several resources can tell them apart.
	Resource string
	Table    string
	// ID names the row this change is about.
	ID string
	// Owner is the row's owner, or empty for a resource with no owner column.
	Owner string
	Op    Op
	// Scope and Actor are the dimensions the call named.
	Scope tenancy.Scope
	Actor Actor
}

// Hook runs inside the transaction that performed a write.
//
// The executor is the point. A hook that published an event or wrote an audit
// entry after the transaction committed would be a second operation against a
// second system with no shared commit: the row lands, the hook fails, and
// durable state diverges from whatever was supposed to describe it. Taking the
// caller's executor makes a hook's writes further statements in the same
// transaction, so they live or die with the row.
//
// The cost is on the same ledger and should be stated plainly: a hook that fails
// fails the write. That is the trade being made deliberately — the alternative is
// the divergence above, undetected.
//
// Hooks run in registration order, and the first error stops the rest and rolls
// the transaction back.
type Hook[T any] func(ctx context.Context, exec database.SQLQueryExecutor, change Change[T]) error
