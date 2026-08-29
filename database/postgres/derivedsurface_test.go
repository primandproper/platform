package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// fakeStdlibConn stands in for *stdlib.Conn, recording the context each
// context-taking method was handed so a test can ask whether the mark arrived.
type fakeStdlibConn struct {
	prepareCtx      context.Context
	execCtx         context.Context
	queryCtx        context.Context
	beginCtx        context.Context
	pingCtx         context.Context
	resetCtx        context.Context
	stmt            driver.Stmt
	prepareErr      error
	closed          bool
	namedValueCheck bool
}

var _ stdlibConn = (*fakeStdlibConn)(nil)

func (c *fakeStdlibConn) Prepare(string) (driver.Stmt, error) { return c.stmt, c.prepareErr }

func (c *fakeStdlibConn) Close() error { c.closed = true; return nil }

func (c *fakeStdlibConn) Begin() (driver.Tx, error) { return nil, nil }

func (c *fakeStdlibConn) PrepareContext(ctx context.Context, _ string) (driver.Stmt, error) {
	c.prepareCtx = ctx

	return c.stmt, c.prepareErr
}

func (c *fakeStdlibConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	c.beginCtx = ctx

	return nil, nil
}

func (c *fakeStdlibConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	c.execCtx = ctx

	return driver.RowsAffected(1), nil
}

func (c *fakeStdlibConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	c.queryCtx = ctx

	return nil, nil
}

func (c *fakeStdlibConn) CheckNamedValue(*driver.NamedValue) error {
	c.namedValueCheck = true

	return nil
}

func (c *fakeStdlibConn) Ping(ctx context.Context) error {
	c.pingCtx = ctx

	return nil
}

func (c *fakeStdlibConn) ResetSession(ctx context.Context) error {
	c.resetCtx = ctx

	return nil
}

// fakeStdlibStmt stands in for *stdlib.Stmt.
type fakeStdlibStmt struct {
	execCtx  context.Context
	queryCtx context.Context
}

var _ stdlibStmt = (*fakeStdlibStmt)(nil)

func (s *fakeStdlibStmt) Close() error { return nil }

func (s *fakeStdlibStmt) NumInput() int { return 0 }

func (s *fakeStdlibStmt) Exec([]driver.Value) (driver.Result, error) { return nil, driver.ErrSkip }

func (s *fakeStdlibStmt) Query([]driver.Value) (driver.Rows, error) { return nil, driver.ErrSkip }

func (s *fakeStdlibStmt) ExecContext(ctx context.Context, _ []driver.NamedValue) (driver.Result, error) {
	s.execCtx = ctx

	return driver.RowsAffected(1), nil
}

func (s *fakeStdlibStmt) QueryContext(ctx context.Context, _ []driver.NamedValue) (driver.Rows, error) {
	s.queryCtx = ctx

	return nil, nil
}

// fakeConnector hands out whatever connection the test gave it.
type fakeConnector struct {
	conn       driver.Conn
	err        error
	connectCtx context.Context
}

var _ driver.Connector = (*fakeConnector)(nil)

func (c *fakeConnector) Connect(ctx context.Context) (driver.Conn, error) {
	c.connectCtx = ctx

	return c.conn, c.err
}

func (c *fakeConnector) Driver() driver.Driver { return nil }

// unmarkableConn satisfies driver.Conn and nothing else, standing in for a pgx
// stdlib connection that stopped presenting the surface the wrapper needs.
type unmarkableConn struct {
	closeErr error
	closed   bool
}

var _ driver.Conn = (*unmarkableConn)(nil)

func (c *unmarkableConn) Prepare(string) (driver.Stmt, error) { return nil, nil }

func (c *unmarkableConn) Close() error { c.closed = true; return c.closeErr }

func (c *unmarkableConn) Begin() (driver.Tx, error) { return nil, nil }

func TestMarkDerivedSurface(T *testing.T) {
	T.Parallel()

	T.Run("an unmarked context is not from the derived surface", func(t *testing.T) {
		t.Parallel()

		test.False(t, fromDerivedSurface(t.Context()))
	})

	T.Run("a marked context is", func(t *testing.T) {
		t.Parallel()

		test.True(t, fromDerivedSurface(markDerivedSurface(t.Context())))
	})

	T.Run("a nil context is not", func(t *testing.T) {
		t.Parallel()

		//nolint:staticcheck // the nil guard is the thing under test.
		test.False(t, fromDerivedSurface(nil))
	})
}

func TestDerivedConnector_Connect(T *testing.T) {
	T.Parallel()

	T.Run("marks the connect and wraps the connection", func(t *testing.T) {
		t.Parallel()

		inner := &fakeStdlibConn{}
		connector := &derivedConnector{Connector: &fakeConnector{conn: inner}}

		conn, err := connector.Connect(t.Context())
		must.NoError(t, err)

		wrapped, ok := conn.(*derivedConn)
		must.True(t, ok)
		test.True(t, wrapped.stdlibConn == stdlibConn(inner))
	})

	T.Run("passes the underlying failure through", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("no route to host")
		connector := &derivedConnector{Connector: &fakeConnector{err: expected}}

		_, err := connector.Connect(t.Context())
		test.ErrorIs(t, err, expected)
	})

	T.Run("refuses and closes a connection it cannot mark", func(t *testing.T) {
		t.Parallel()

		inner := &unmarkableConn{}
		connector := &derivedConnector{Connector: &fakeConnector{conn: inner}}

		_, err := connector.Connect(t.Context())
		test.Error(t, err)
		test.True(t, inner.closed)
	})

	T.Run("reports the close failure alongside the refusal", func(t *testing.T) {
		t.Parallel()

		closeErr := errors.New("already gone")
		connector := &derivedConnector{Connector: &fakeConnector{conn: &unmarkableConn{closeErr: closeErr}}}

		_, err := connector.Connect(t.Context())
		test.ErrorIs(t, err, closeErr)
	})
}

func TestDerivedConn(T *testing.T) {
	T.Parallel()

	buildConn := func(t *testing.T) (*derivedConn, *fakeStdlibConn, *fakeStdlibStmt) {
		t.Helper()

		stmt := &fakeStdlibStmt{}
		inner := &fakeStdlibConn{stmt: stmt}

		return &derivedConn{stdlibConn: inner}, inner, stmt
	}

	T.Run("marks every context-taking call", func(t *testing.T) {
		t.Parallel()

		conn, inner, _ := buildConn(t)

		_, err := conn.ExecContext(t.Context(), "UPDATE a SET b = 1", nil)
		must.NoError(t, err)
		test.True(t, fromDerivedSurface(inner.execCtx))

		rows, err := conn.QueryContext(t.Context(), "SELECT 1", nil)
		must.NoError(t, err)
		test.Nil(t, rows)
		test.True(t, fromDerivedSurface(inner.queryCtx))

		_, err = conn.BeginTx(t.Context(), driver.TxOptions{})
		must.NoError(t, err)
		test.True(t, fromDerivedSurface(inner.beginCtx))

		must.NoError(t, conn.Ping(t.Context()))
		test.True(t, fromDerivedSurface(inner.pingCtx))

		must.NoError(t, conn.ResetSession(t.Context()))
		test.True(t, fromDerivedSurface(inner.resetCtx))

		_, err = conn.PrepareContext(t.Context(), "SELECT 1")
		must.NoError(t, err)
		test.True(t, fromDerivedSurface(inner.prepareCtx))
	})

	T.Run("the context-free Prepare goes through the marking one", func(t *testing.T) {
		t.Parallel()

		conn, inner, _ := buildConn(t)

		stmt, err := conn.Prepare("SELECT 1")
		must.NoError(t, err)
		test.True(t, fromDerivedSurface(inner.prepareCtx))

		_, ok := stmt.(*derivedStmt)
		test.True(t, ok)
	})

	T.Run("a prepared statement marks its own executions", func(t *testing.T) {
		t.Parallel()

		conn, _, innerStmt := buildConn(t)

		stmt, err := conn.PrepareContext(t.Context(), "SELECT 1")
		must.NoError(t, err)

		wrapped, ok := stmt.(*derivedStmt)
		must.True(t, ok)

		_, err = wrapped.ExecContext(t.Context(), nil)
		must.NoError(t, err)
		test.True(t, fromDerivedSurface(innerStmt.execCtx))

		rows, err := wrapped.QueryContext(t.Context(), nil)
		must.NoError(t, err)
		test.Nil(t, rows)
		test.True(t, fromDerivedSurface(innerStmt.queryCtx))
	})

	T.Run("a prepare failure is passed through unwrapped", func(t *testing.T) {
		t.Parallel()

		expected := errors.New("syntax error")
		conn := &derivedConn{stdlibConn: &fakeStdlibConn{prepareErr: expected}}

		stmt, err := conn.PrepareContext(t.Context(), "SELEKT 1")
		test.ErrorIs(t, err, expected)
		test.Nil(t, stmt)
	})

	T.Run("keeps the optional capabilities database/sql discovers by assertion", func(t *testing.T) {
		t.Parallel()

		conn, inner, _ := buildConn(t)

		checker, ok := driver.Conn(conn).(driver.NamedValueChecker)
		must.True(t, ok)
		must.NoError(t, checker.CheckNamedValue(&driver.NamedValue{}))
		test.True(t, inner.namedValueCheck)

		_, ok = driver.Conn(conn).(driver.ExecerContext)
		test.True(t, ok)

		_, ok = driver.Conn(conn).(driver.QueryerContext)
		test.True(t, ok)

		_, ok = driver.Conn(conn).(driver.ConnBeginTx)
		test.True(t, ok)

		_, ok = driver.Conn(conn).(driver.ConnPrepareContext)
		test.True(t, ok)

		_, ok = driver.Conn(conn).(driver.Pinger)
		test.True(t, ok)

		_, ok = driver.Conn(conn).(driver.SessionResetter)
		test.True(t, ok)
	})
}
