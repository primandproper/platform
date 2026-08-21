package writes

import (
	"context"

	"github.com/primandproper/platform-go/v12/database"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/tenancy"
)

// ErrUnknownOp indicates a Change whose Op is not one of the three below.
//
// It is separate from the empty-field failures because it is a different
// mistake: an empty Op is a caller who forgot to say what happened, and an
// unrecognized one is a caller who invented a vocabulary the hooks downstream do
// not read.
var ErrUnknownOp = platformerrors.Wrap(platformerrors.ErrUnrecognizedInputValue, "unknown write operation")

// Op names what a write did.
type Op string

const (
	// OpCreated is a row that did not exist before.
	OpCreated Op = "created"
	// OpUpdated is a row whose mutable columns were reassigned.
	OpUpdated Op = "updated"
	// OpArchived is a row that was deleted, whether hard, soft, singly, or as
	// part of a cascade.
	OpArchived Op = "archived"
)

// Valid reports whether op is one of the three named above.
func (o Op) Valid() bool {
	switch o {
	case OpCreated, OpUpdated, OpArchived:
		return true
	default:
		return false
	}
}

// Change describes one write, for the hooks that run inside the transaction
// that performed it.
//
// It is identity and not content. A hook that wants the row itself closes over
// it — the caller building this is inside the closure that just wrote it — and
// keeping the row out of here is what lets Change, Hook and Writer stay free of
// a type parameter that every consumer would otherwise have to spell.
//
// A cascade reports one Change per row rather than one for the statement. An
// audit log that records that a cascade happened, rather than what it touched,
// is a log that cannot answer the only question anybody asks it afterwards.
//
// Hooks receive it by pointer, because at a hundred bytes it is past the size
// this module copies by value. It is the write's account of itself and not a
// working value: a hook that reassigns a field is editing what the hooks after
// it will be told, and what the write did is not theirs to revise.
type Change struct {
	// Resource is what the row is called in the application's own vocabulary —
	// "service_setting", "comment" — so one hook registered across several
	// domains can tell them apart. Required.
	Resource string

	// Table is the table the row lives in, for a hook keyed by storage rather
	// than by domain. Optional: for most resources it is Resource pluralized,
	// and a hook that does not read it should not have to be told it.
	Table string

	// ID names the row this change is about. Required — a change that names no
	// row is an audit entry that describes nothing.
	ID string

	// Owner is the row's owner, or empty for a resource with no owner column.
	// This is authorship — which user wrote the row — and not tenancy, which is
	// Scope.
	Owner string

	// Op is what happened to the row. Required.
	Op Op

	// Scope is the tenancy dimension the write named.
	//
	// It has no usable zero value on purpose, and Validate rejects one: a hook
	// that writes an audit row or an outbox event is storing consumer data, and
	// the scope column on that row is not optional. An application whose rows
	// belong to no tenant says tenancy.Global() and means it.
	Scope tenancy.Scope
}

// Validate reports a Change that cannot be recorded: no resource, no id, no
// operation, or an unset scope.
//
// Writer.Do validates every change before any hook runs, and it does so whether
// or not any hooks are registered. Validating only when somebody is listening
// would mean that attaching the first hook to a working application is the
// change that breaks it, at a call site nobody edited.
func (c *Change) Validate() error {
	if c.Resource == "" {
		return platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "change names no resource")
	}

	if c.ID == "" {
		return platformerrors.Wrap(platformerrors.ErrInvalidIDProvided, "change names no row")
	}

	if !c.Op.Valid() {
		return platformerrors.Wrapf(ErrUnknownOp, "%q", c.Op)
	}

	if err := c.Scope.Validate(); err != nil {
		return platformerrors.Wrapf(err, "change to %s %s", c.Resource, c.ID)
	}

	return nil
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
// fails the write. That is the trade being made deliberately — the alternative
// is the divergence above, undetected. A side effect that should not be able to
// fail a write does not belong in a hook; it belongs after Do returns.
//
// Hooks run in registration order for each change, and changes in the order the
// write reported them. The first error stops the rest and rolls the transaction
// back.
type Hook func(ctx context.Context, exec database.SQLQueryExecutor, change *Change) error
