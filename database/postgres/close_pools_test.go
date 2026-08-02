package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// closingConnector is a driver.Connector whose Close reports closeErr, so the
// failure branches of closePools can be exercised without a live database.
type closingConnector struct {
	closeErr error
	closed   *int
}

func (c closingConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("closingConnector does not connect")
}

func (closingConnector) Driver() driver.Driver { return nil }

func (c closingConnector) Close() error {
	*c.closed++

	return c.closeErr
}

func newDB(t *testing.T, closeErr error, closed *int) *sql.DB {
	t.Helper()

	return sql.OpenDB(closingConnector{closeErr: closeErr, closed: closed})
}

// newPool builds a pgxpool that parses but never dials, which is all closePools
// needs — Close on an idle pool does not touch the network.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(t.Context(), "postgres://user:pass@127.0.0.1:1/db")
	must.NoError(t, err)

	return pool
}

func TestClosePools(T *testing.T) {
	T.Parallel()

	T.Run("nil handles are skipped", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("original")

		test.ErrorIs(t, closePools(cause, nil, nil, nil, nil), cause)
	})

	T.Run("the original cause is always preserved", func(t *testing.T) {
		t.Parallel()

		var closed int
		cause := errors.New("original")
		db := newDB(t, nil, &closed)

		err := closePools(cause, db, db, nil, nil)
		test.ErrorIs(t, err, cause)
		// Shared read/write handle: closed once, not twice.
		test.EqOp(t, 1, closed)
	})

	T.Run("distinct handles are both closed", func(t *testing.T) {
		t.Parallel()

		var readClosed, writeClosed int
		readDB := newDB(t, nil, &readClosed)
		writeDB := newDB(t, nil, &writeClosed)

		must.Error(t, closePools(errors.New("original"), readDB, writeDB, nil, nil))
		test.EqOp(t, 1, readClosed)
		test.EqOp(t, 1, writeClosed)
	})

	T.Run("a close failure is joined onto the cause rather than replacing it", func(t *testing.T) {
		t.Parallel()

		var readClosed, writeClosed int
		cause := errors.New("original")
		readCloseErr := errors.New("read close failed")
		writeCloseErr := errors.New("write close failed")

		readDB := newDB(t, readCloseErr, &readClosed)
		writeDB := newDB(t, writeCloseErr, &writeClosed)

		// Losing the cause here would report a cleanup problem in place of the
		// failure that triggered the cleanup.
		err := closePools(cause, readDB, writeDB, nil, nil)
		test.ErrorIs(t, err, cause)
		test.ErrorIs(t, err, readCloseErr)
		test.ErrorIs(t, err, writeCloseErr)
	})

	T.Run("pools are closed alongside the handles", func(t *testing.T) {
		t.Parallel()

		var closed int
		cause := errors.New("original")
		db := newDB(t, nil, &closed)
		readPool, writePool := newPool(t), newPool(t)

		test.ErrorIs(t, closePools(cause, db, db, readPool, writePool), cause)
	})

	T.Run("a shared pool is closed once", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("original")
		pool := newPool(t)

		// pgxpool.Close panics on a second call, so passing the same pool as both
		// read and write is the assertion: it must be closed exactly once.
		test.ErrorIs(t, closePools(cause, nil, nil, pool, pool), cause)
	})
}
