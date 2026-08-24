package database_test

import (
	"context"
	stderrors "errors"
	"os/exec"
	"sync"
	"testing"

	"github.com/primandproper/platform-go/v13/database"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// escapedTx captures the Tx a callback was handed so a test can use it after the
// callback has returned, which is the escape RunInTransaction cannot prevent and
// the spent flag exists to make legible.
func escapedTx(t *testing.T) (database.Tx, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	must.NoError(t, err)

	mock.ExpectBegin()
	mock.ExpectCommit()

	var escaped database.Tx

	rb := func(_ context.Context, tx database.SQLQueryExecutorAndTransactionManager) { _ = tx.Rollback() }

	must.NoError(t, database.RunInTransaction(t.Context(), db, rb, func(tx database.Tx) error {
		escaped = tx

		return nil
	}))
	must.NotNil(t, escaped)

	return escaped, mock
}

func TestTx_SpentAfterTheCallbackReturns(T *testing.T) {
	T.Parallel()

	// Every method, not a representative one: a Tx that refused three of four
	// would leave the fourth reaching a transaction that has been committed,
	// and which of the four a caller reaches for is not something the type
	// controls.

	T.Run("ExecContext", func(t *testing.T) {
		t.Parallel()

		tx, mock := escapedTx(t)

		result, err := tx.ExecContext(t.Context(), "UPDATE things SET x = 1")
		test.Nil(t, result)
		test.ErrorIs(t, err, database.ErrTransactionClosed)

		// Nothing reached the driver, which is the point: the statement was not
		// run against a committed transaction and quietly ignored.
		must.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("QueryContext", func(t *testing.T) {
		t.Parallel()

		tx, mock := escapedTx(t)

		rows, err := tx.QueryContext(t.Context(), "SELECT 1")
		test.Nil(t, rows)
		test.ErrorIs(t, err, database.ErrTransactionClosed)
		must.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("PrepareContext", func(t *testing.T) {
		t.Parallel()

		tx, mock := escapedTx(t)

		stmt, err := tx.PrepareContext(t.Context(), "SELECT 1")
		test.Nil(t, stmt)
		test.ErrorIs(t, err, database.ErrTransactionClosed)
		must.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("QueryRowContext reports through the Row it returns", func(t *testing.T) {
		t.Parallel()

		// *sql.Row has no exported constructor and carries its error in an
		// unexported field, so this method cannot return the sentinel directly.
		// Both ways of reading a Row have to surface it anyway.
		tx, mock := escapedTx(t)

		row := tx.QueryRowContext(t.Context(), "SELECT 1")
		must.NotNil(t, row)
		test.ErrorIs(t, row.Err(), database.ErrTransactionClosed)

		var n int
		test.ErrorIs(t, tx.QueryRowContext(t.Context(), "SELECT 1").Scan(&n), database.ErrTransactionClosed)

		must.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestTx_SpentBeforeTheOutcomeIsDecided(T *testing.T) {
	T.Parallel()

	T.Run("a goroutine holding the Tx past the callback races nothing", func(t *testing.T) {
		t.Parallel()

		// The lifetime case: a callback starts work that outlives it. The write
		// is lost either way — this asserts it is lost as an error rather than
		// as a data race, which is what -race in the test command grades.
		db, mock, err := sqlmock.New()
		must.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectCommit()

		var (
			wg      sync.WaitGroup
			lateErr error
			release = make(chan struct{})
		)

		rb := func(_ context.Context, tx database.SQLQueryExecutorAndTransactionManager) { _ = tx.Rollback() }

		must.NoError(t, database.RunInTransaction(t.Context(), db, rb, func(tx database.Tx) error {
			wg.Go(func() {
				<-release

				_, lateErr = tx.ExecContext(context.WithoutCancel(t.Context()), "UPDATE things SET x = 1")
			})

			return nil
		}))

		close(release)
		wg.Wait()

		test.ErrorIs(t, lateErr, database.ErrTransactionClosed)
		must.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("a Tx that escaped a failed callback is spent too", func(t *testing.T) {
		t.Parallel()

		db, mock, err := sqlmock.New()
		must.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectRollback()

		var escaped database.Tx

		rb := func(_ context.Context, tx database.SQLQueryExecutorAndTransactionManager) { _ = tx.Rollback() }

		sentinel := database.ErrDatabaseNotReady
		test.ErrorIs(t, database.RunInTransaction(t.Context(), db, rb, func(tx database.Tx) error {
			escaped = tx

			return sentinel
		}), sentinel)

		_, execErr := escaped.ExecContext(t.Context(), "UPDATE things SET x = 1")
		test.ErrorIs(t, execErr, database.ErrTransactionClosed)
		must.NoError(t, mock.ExpectationsWereMet())
	})

	T.Run("a panicking callback spends its Tx on the way out", func(t *testing.T) {
		t.Parallel()

		db, mock, err := sqlmock.New()
		must.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectRollback()

		var escaped database.Tx

		rb := func(_ context.Context, tx database.SQLQueryExecutorAndTransactionManager) { _ = tx.Rollback() }

		func() {
			defer func() { must.NotNil(t, recover()) }()

			_ = database.RunInTransaction(t.Context(), db, rb, func(tx database.Tx) error {
				escaped = tx

				panic("a domain's bug")
			})
		}()

		_, execErr := escaped.ExecContext(t.Context(), "UPDATE things SET x = 1")
		test.ErrorIs(t, execErr, database.ErrTransactionClosed)
		must.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestTx_LiveInsideTheCallback(T *testing.T) {
	T.Parallel()

	T.Run("every method reaches the transaction while the callback runs", func(t *testing.T) {
		t.Parallel()

		db, mock, err := sqlmock.New()
		must.NoError(t, err)

		mock.ExpectBegin()
		mock.ExpectExec("UPDATE things").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a"))
		mock.ExpectQuery("SELECT count").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(7))
		mock.ExpectPrepare("INSERT INTO things")
		mock.ExpectCommit()

		rb := func(_ context.Context, tx database.SQLQueryExecutorAndTransactionManager) { _ = tx.Rollback() }

		must.NoError(t, database.RunInTransaction(t.Context(), db, rb, func(tx database.Tx) error {
			ctx := t.Context()

			result, execErr := tx.ExecContext(ctx, "UPDATE things SET x = 1")
			must.NoError(t, execErr)
			must.NotNil(t, result)

			rows, queryErr := tx.QueryContext(ctx, "SELECT id FROM things")
			must.NoError(t, queryErr)

			defer func() { must.NoError(t, rows.Close()) }()

			var n int
			must.NoError(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM things").Scan(&n))
			test.EqOp(t, 7, n)

			stmt, prepErr := tx.PrepareContext(ctx, "INSERT INTO things (id) VALUES (?)")
			must.NoError(t, prepErr)

			defer func() { must.NoError(t, stmt.Close()) }()

			return nil
		}))

		must.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestNewTxForTesting(T *testing.T) {
	T.Parallel()

	T.Run("presents an ordinary executor as a Tx", func(t *testing.T) {
		t.Parallel()

		db, mock, err := sqlmock.New()
		must.NoError(t, err)

		mock.ExpectExec("UPDATE things").WillReturnResult(sqlmock.NewResult(1, 1))

		tx := database.NewTxForTesting(db)
		must.NotNil(t, tx)

		result, execErr := tx.ExecContext(t.Context(), "UPDATE things SET x = 1")
		must.NoError(t, execErr)
		must.NotNil(t, result)

		// Nothing spends it: there is no callback for it to outlive. A second
		// call is refused by sqlmock, which has no expectation left — not by
		// the spent flag, which never gets set on a Tx made this way.
		_, execErr = tx.ExecContext(t.Context(), "UPDATE things SET x = 1")
		must.Error(t, execErr)
		test.False(t, stderrors.Is(execErr, database.ErrTransactionClosed))
	})
}

// TestWriterIsNotATx is this module's negative-compile check, and the mechanism
// the issue behind Tx asked to be settled rather than left open.
//
// Go has no standard way to assert that something does not compile, and the two
// candidates were a documented example and a build that must fail. An example
// goes stale in silence — the day a refactor makes Writer assignable to a Tx
// parameter again, the prose still says it does not compile and nothing
// disagrees. This fails instead.
//
// The fixture lives in database/testdata, which the go tool skips for wildcard
// patterns, so it is invisible to `go build ./...` and reachable only by the
// explicit path below.
func TestWriterIsNotATx(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("compiles a package")
	}

	cmd := exec.CommandContext(t.Context(), "go", "build", "./testdata/writerisnottx")
	out, err := cmd.CombinedOutput()

	// The assertion is the failure. A nil error here means Client.Writer() is
	// once again assignable to a parameter that documents itself as
	// transactional, which is the whole defect database.Tx exists to close.
	must.Error(t, err)

	// And it must fail for the stated reason rather than because the fixture
	// stopped naming a real API — a package rename would otherwise leave this
	// test green on "undefined: outbox.Writer".
	rendered := string(out)
	for _, want := range []string{
		"does not implement database.Tx",
		"in argument to w.Enqueue",
		"in argument to store.CreateUser",
	} {
		test.StrContains(t, rendered, want)
	}
}

// A Tx is usable everywhere a bare executor is, which is what keeps the
// unexported helpers that take SQLQueryExecutor reachable from inside a
// transaction.
var _ database.SQLQueryExecutor = database.Tx(nil)
