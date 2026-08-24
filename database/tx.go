package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"sync/atomic"
)

type (
	// Tx is the executor a transaction hands to its callback. It is a SQLQueryExecutor
	// plus one unexported marker method, which is the whole point: only this package can
	// produce a Tx, and the only thing in this package that produces one is
	// RunInTransaction. So a parameter typed Tx says "this runs in a transaction" in a
	// way the compiler enforces, where a SQLQueryExecutor parameter said it in prose and
	// accepted Client.Writer() without complaint.
	//
	// That distinction is why several exported entry points in this module take a Tx
	// rather than a SQLQueryExecutor — identity's Registrar trio, identity's
	// AcceptInvitation and EraseUser, outbox's Enqueue and SideEffect. Each writes rows
	// that are only meaningful together, and each previously carried the obligation as a
	// doc comment that a caller holding Writer() could satisfy by not reading it.
	//
	// Unexported helpers that are correct in either context keep SQLQueryExecutor. A
	// helper called both from inside a WithTransaction and from a single-statement write
	// genuinely does not care, and narrowing it to Tx would force its non-transactional
	// callers to open a transaction they do not need.
	//
	// # What this does not catch
	//
	// Three misuses survive the type, and naming them here is cheaper than letting a
	// reader discover them:
	//
	// Escape. A callback can store its Tx in a struct field, a package variable, or a
	// closure that outlives the callback. The type cannot stop that. Use after the
	// callback returns is caught at runtime instead: the Tx is marked spent the instant
	// the callback returns, and every method on a spent Tx fails with
	// ErrTransactionClosed rather than reaching a transaction that has already been
	// committed or rolled back.
	//
	// Lifetime. A goroutine started inside the callback can outlive it and reach for the
	// Tx while the transaction is being committed. The spent flag is atomic precisely so
	// that this is a clean ErrTransactionClosed under the race detector rather than a
	// data race, but the write it was trying to make is still lost. *sql.Tx is not safe
	// for concurrent use by design; a Tx inherits that, and the flag only makes the
	// failure legible.
	//
	// Nesting. Client.WithTransaction takes no executor, so calling it from inside a
	// callback opens a second, independent transaction on the write pool rather than a
	// savepoint. The two commit separately and can deadlock against each other. Nothing
	// in the type system says so, because the inner call does not mention the outer Tx.
	//
	// A fourth is worth stating even though it is not a misuse of Tx: holding a Tx does
	// not stop the same code from also calling Client.Writer(). Statements sent through
	// the writer are outside the transaction and commit on their own.
	Tx interface {
		SQLQueryExecutor

		// isTx marks an executor as a transaction. It exists to be unimplementable
		// outside this package, and has no behavior.
		isTx()
	}

	// txExecutor is the only implementation of Tx. It wraps the executor a transaction
	// runs on and refuses every method once the transaction's callback has returned.
	txExecutor struct {
		exec  SQLQueryExecutor
		spent atomic.Bool
	}

	// closedTxConnector is a driver.Connector that never yields a connection, failing
	// with ErrTransactionClosed instead.
	//
	// It exists for one method. *sql.Row carries its error in an unexported field and
	// there is no constructor for one, so QueryRowContext on a spent Tx cannot simply
	// return an errored Row the way the other three methods return an error. Handing
	// back a Row from a *sql.DB whose connector fails with the sentinel produces exactly
	// that Row: both Err and Scan report ErrTransactionClosed. The alternative was to
	// let QueryRowContext through to a finished transaction and report whatever the
	// driver said — sql.ErrTxDone on a good day, a nil-pointer panic on a hand-rolled
	// executor — which would make one of the four methods quietly weaker than the
	// others.
	closedTxConnector struct{}
)

var (
	_ Tx = (*txExecutor)(nil)

	// closedTxDB yields Rows pre-loaded with ErrTransactionClosed. It opens no
	// connection and needs no cleanup: sql.OpenDB is lazy, and this connector fails
	// before a driver.Conn exists.
	closedTxDB = sql.OpenDB(closedTxConnector{})
)

// Connect never connects.
func (closedTxConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, ErrTransactionClosed
}

// Driver returns nil. database/sql only calls it to satisfy DB.Driver, which nothing
// here does, and the connector fails before a driver is needed.
func (closedTxConnector) Driver() driver.Driver { return nil }

// newTxExecutor wraps exec as the Tx for one transaction.
func newTxExecutor(exec SQLQueryExecutor) *txExecutor {
	return &txExecutor{exec: exec}
}

// spend marks the transaction finished. Every subsequent call on this Tx fails with
// ErrTransactionClosed. It is called once, by RunInTransaction, the moment the callback
// returns — before the commit or rollback that follows, so a Tx that escaped the
// callback cannot race the outcome.
func (t *txExecutor) spend() { t.spent.Store(true) }

func (*txExecutor) isTx() {}

// ExecContext runs a statement on the transaction, or fails if it has finished.
func (t *txExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if t.spent.Load() {
		return nil, ErrTransactionClosed
	}

	return t.exec.ExecContext(ctx, query, args...)
}

// PrepareContext prepares a statement on the transaction, or fails if it has finished.
func (t *txExecutor) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if t.spent.Load() {
		return nil, ErrTransactionClosed
	}

	return t.exec.PrepareContext(ctx, query)
}

// QueryContext runs a query on the transaction, or fails if it has finished.
func (t *txExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if t.spent.Load() {
		return nil, ErrTransactionClosed
	}

	return t.exec.QueryContext(ctx, query, args...)
}

// QueryRowContext runs a single-row query on the transaction. Once the transaction has
// finished it returns a *sql.Row whose Err and Scan both report ErrTransactionClosed —
// see closedTxConnector for why this method cannot simply return the error.
func (t *txExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if t.spent.Load() {
		return closedTxDB.QueryRowContext(ctx, query, args...)
	}

	return t.exec.QueryRowContext(ctx, query, args...)
}

// NewTxForTesting presents exec as a Tx without a transaction behind it. It is an escape
// hatch for tests, and it is the one hole in the guarantee Tx exists to make: whatever
// this is handed — a mock, a bare *sql.DB, a Client's Writer — becomes assignable to
// every parameter in this module that says "must run in a transaction".
//
// It exists because the alternative is worse. Fifteen hand-written Client doubles in this
// module implement WithTransaction by invoking the callback with an executor they made up,
// and without a constructor each of them would either reimplement Tx (they cannot; the
// marker is unexported) or the doubles would have to become real database connections.
// Consumers writing their own doubles are in the same position.
//
// Production code must not call it. forbidigo enforces that in this repository — the rule
// is relaxed for _test.go files and nothing else — and a consumer that wants the same
// guarantee can add the same three lines to its own .golangci.yml.
//
// The Tx it returns is spent-checked like any other, but nothing ever spends it: there is
// no callback to return from. A test that wants to observe ErrTransactionClosed needs a
// real WithTransaction.
func NewTxForTesting(exec SQLQueryExecutor) Tx {
	return newTxExecutor(exec)
}
