package postgres

import (
	"context"
	"database/sql/driver"

	"github.com/primandproper/platform-go/v14/errors"

	"github.com/jackc/pgx/v5/stdlib"
)

// The database/sql handles this package hands out are derived from the same pgx
// pools PgxAccess exposes, which means the stdlib driver runs their statements
// through the same *pgx.Conn — and therefore through the same pgx tracer. Left
// alone, every Reader/Writer query would be spanned twice: once by otelsql at
// the database/sql layer, once by pgxTracer underneath it.
//
// Rather than rule the native path untraceable, the client marks the contexts
// it owns. Every context-taking method of the derived driver connection stamps
// a value on the way down, and pgxTracer skips any call carrying it. What is
// left unmarked is exactly what came in through a pool the caller took from
// PgxAccess.
//
// The marking is a wrapper around pgx's own connector rather than a fork of it:
// derivedConn and derivedStmt embed the surfaces *stdlib.Conn and *stdlib.Stmt
// present to database/sql, so every method not listed here is the one pgx
// wrote.

// derivedSurfaceKey marks a context as belonging to the derived database/sql
// surface.
type derivedSurfaceKey struct{}

// markDerivedSurface stamps ctx as belonging to the derived database/sql
// surface.
func markDerivedSurface(ctx context.Context) context.Context {
	return context.WithValue(ctx, derivedSurfaceKey{}, struct{}{})
}

// fromDerivedSurface reports whether ctx was stamped by the derived
// database/sql surface, and so has already been spanned by otelsql.
func fromDerivedSurface(ctx context.Context) bool {
	if ctx == nil {
		return false
	}

	return ctx.Value(derivedSurfaceKey{}) != nil
}

// stdlibConn is the driver surface *stdlib.Conn presents to database/sql. It is
// spelled out so that derivedConn can embed it: every optional interface
// database/sql discovers by assertion has to survive the wrapping, and an
// interface enumerating them fails to compile when it does not rather than
// quietly costing the derived handle a capability.
type stdlibConn interface {
	driver.Conn
	driver.ConnPrepareContext
	driver.ConnBeginTx
	driver.ExecerContext
	driver.QueryerContext
	driver.NamedValueChecker
	driver.Pinger
	driver.SessionResetter
}

// stdlibStmt is the driver surface *stdlib.Stmt presents to database/sql.
type stdlibStmt interface {
	driver.Stmt
	driver.StmtExecContext
	driver.StmtQueryContext
}

var (
	_ stdlibConn       = (*stdlib.Conn)(nil)
	_ stdlibStmt       = (*stdlib.Stmt)(nil)
	_ driver.Connector = (*derivedConnector)(nil)
	_ stdlibConn       = (*derivedConn)(nil)
	_ stdlibStmt       = (*derivedStmt)(nil)
)

// derivedConnector wraps pgx's pool connector so the connections it hands
// database/sql mark the contexts they are given.
type derivedConnector struct {
	driver.Connector
}

// Connect satisfies driver.Connector.
func (c *derivedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(markDerivedSurface(ctx))
	if err != nil {
		return nil, err
	}

	inner, ok := conn.(stdlibConn)
	if !ok {
		err = errors.Newf("pgx stdlib connector returned a %T, which the derived surface cannot mark", conn)
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, errors.Wrap(closeErr, "closing unrecognized driver connection"))
		}

		return nil, err
	}

	return &derivedConn{stdlibConn: inner}, nil
}

// derivedConn is the driver connection database/sql holds. It is pgx's, with
// every context-taking method stamping the mark on the way through.
type derivedConn struct {
	stdlibConn
}

// Prepare satisfies driver.Conn. database/sql prefers PrepareContext; this is
// the same call with the background context pgx would have supplied, kept so
// that the statement it returns is wrapped like any other.
func (c *derivedConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext satisfies driver.ConnPrepareContext.
func (c *derivedConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.stdlibConn.PrepareContext(markDerivedSurface(ctx), query)
	if err != nil {
		return nil, err
	}

	inner, ok := stmt.(stdlibStmt)
	if !ok {
		// Unreachable against pgx, whose Stmt is asserted to satisfy stdlibStmt
		// above. An implementation that ever stopped would lose the mark on
		// prepared statements only, which costs a duplicate span rather than a
		// working handle, so it is returned rather than refused.
		return stmt, nil
	}

	return &derivedStmt{stdlibStmt: inner}, nil
}

// ExecContext satisfies driver.ExecerContext.
func (c *derivedConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.stdlibConn.ExecContext(markDerivedSurface(ctx), query, args)
}

// QueryContext satisfies driver.QueryerContext.
func (c *derivedConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.stdlibConn.QueryContext(markDerivedSurface(ctx), query, args)
}

// BeginTx satisfies driver.ConnBeginTx. pgx issues BEGIN, COMMIT, and ROLLBACK
// as ordinary statements on the context it was handed here, so marking this one
// covers the whole transaction.
func (c *derivedConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.stdlibConn.BeginTx(markDerivedSurface(ctx), opts)
}

// Ping satisfies driver.Pinger.
func (c *derivedConn) Ping(ctx context.Context) error {
	return c.stdlibConn.Ping(markDerivedSurface(ctx))
}

// ResetSession satisfies driver.SessionResetter.
func (c *derivedConn) ResetSession(ctx context.Context) error {
	return c.stdlibConn.ResetSession(markDerivedSurface(ctx))
}

// derivedStmt is a prepared statement on a derivedConn, marking the contexts
// its executions are given.
type derivedStmt struct {
	stdlibStmt
}

// ExecContext satisfies driver.StmtExecContext.
func (s *derivedStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.stdlibStmt.ExecContext(markDerivedSurface(ctx), args)
}

// QueryContext satisfies driver.StmtQueryContext.
func (s *derivedStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.stdlibStmt.QueryContext(markDerivedSurface(ctx), args)
}
