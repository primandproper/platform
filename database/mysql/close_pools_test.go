package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/shoenig/test"
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

func TestClosePools(T *testing.T) {
	T.Parallel()

	T.Run("nil handles are skipped", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("original")

		test.ErrorIs(t, closePools(cause, nil, nil), cause)
	})

	T.Run("a shared read/write handle is closed once", func(t *testing.T) {
		t.Parallel()

		var closed int
		cause := errors.New("original")
		db := newDB(t, nil, &closed)

		test.ErrorIs(t, closePools(cause, db, db), cause)
		test.EqOp(t, 1, closed)
	})

	T.Run("distinct handles are both closed", func(t *testing.T) {
		t.Parallel()

		var readClosed, writeClosed int
		cause := errors.New("original")

		test.ErrorIs(t, closePools(cause, newDB(t, nil, &readClosed), newDB(t, nil, &writeClosed)), cause)
		test.EqOp(t, 1, readClosed)
		test.EqOp(t, 1, writeClosed)
	})

	T.Run("a close failure is joined onto the cause rather than replacing it", func(t *testing.T) {
		t.Parallel()

		var readClosed, writeClosed int
		cause := errors.New("original")
		readCloseErr := errors.New("read close failed")
		writeCloseErr := errors.New("write close failed")

		// Losing the cause here would report a cleanup problem in place of the
		// failure that triggered the cleanup.
		err := closePools(cause, newDB(t, readCloseErr, &readClosed), newDB(t, writeCloseErr, &writeClosed))
		test.ErrorIs(t, err, cause)
		test.ErrorIs(t, err, readCloseErr)
		test.ErrorIs(t, err, writeCloseErr)
	})
}
