package database

import (
	"context"
	"database/sql"

	platformerrors "github.com/primandproper/platform-go/v14/errors"
)

// RunInTransaction begins a transaction on writeDB, invokes fn with that transaction as
// the sole query executor, and commits when fn returns nil. It is the shared engine
// behind each Client's WithTransaction method — application code should prefer
// Client.WithTransaction, which wraps this with the implementation's observability.
//
// fn receives the transaction as a Tx, not the transaction handle: it cannot commit or
// roll back, and its statements cannot accidentally target the read replica or another
// connection. Tx is producible only here, so a parameter typed Tx anywhere in this module
// is a compile-time claim that the caller is inside one of these. Lifecycle is managed
// entirely here:
//
//   - rollback is invoked (with the transaction) on any non-nil error from fn, and the
//     error is returned unwrapped.
//   - a panic inside fn triggers rollback and is then re-raised, so no connection leaks
//     and the caller still observes the failure.
//   - a nil return from fn commits; commit errors are wrapped and returned.
//
// A failed commit has already released the connection back to the pool, so no second
// rollback is attempted (it would only surface a spurious ErrTxDone).
//
// The Tx is spent the moment fn returns — before the commit or rollback below — so a Tx
// that escaped into a struct field or a goroutine reports ErrTransactionClosed rather
// than racing the outcome of a transaction it can no longer affect.
func RunInTransaction(
	ctx context.Context,
	writeDB *sql.DB,
	rollback func(ctx context.Context, tx SQLQueryExecutorAndTransactionManager),
	fn func(tx Tx) error,
) error {
	if writeDB == nil || rollback == nil || fn == nil {
		return platformerrors.ErrNilInputParameter
	}

	tx, err := writeDB.BeginTx(ctx, nil)
	if err != nil {
		return platformerrors.Wrap(err, "beginning transaction")
	}

	// Roll back on panic and re-raise so the caller still sees the failure and the
	// pooled connection is not leaked.
	defer func() {
		if r := recover(); r != nil {
			rollback(ctx, tx)
			panic(r)
		}
	}()

	txExec := newTxExecutor(tx)

	fnErr := func() error {
		defer txExec.spend()

		return fn(txExec)
	}()
	if fnErr != nil {
		rollback(ctx, tx)

		return fnErr
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return platformerrors.Wrap(commitErr, "committing transaction")
	}

	return nil
}
