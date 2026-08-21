package writes_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v12/database/writes"
	platformerrors "github.com/primandproper/platform-go/v12/errors"

	"github.com/shoenig/test"
)

func TestErrNoRowsAffected(T *testing.T) {
	T.Parallel()

	T.Run("matches sql.ErrNoRows", func(t *testing.T) {
		t.Parallel()

		// The mappers in errors/http and errors/grpc turn sql.ErrNoRows into a
		// 404 and codes.NotFound. A sentinel that did not wrap it would make
		// every missed write a 500.
		test.ErrorIs(t, writes.ErrNoRowsAffected, sql.ErrNoRows)
	})
}

func TestRequireAffected(T *testing.T) {
	T.Parallel()

	T.Run("a row was written", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, writes.RequireAffected(1))
	})

	T.Run("nothing matched", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, writes.RequireAffected(0), writes.ErrNoRowsAffected)
	})

	T.Run("an unknown count is not a miss", func(t *testing.T) {
		t.Parallel()

		// Drivers that cannot report a count return -1. Treating that as zero
		// would fail writes that succeeded.
		test.NoError(t, writes.RequireAffected(-1))
	})
}

func TestRequireAffectedResult(T *testing.T) {
	T.Parallel()

	T.Run("a row was written", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, writes.RequireAffectedResult(affectedResult{affected: 1}))
	})

	T.Run("nothing matched", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, writes.RequireAffectedResult(affectedResult{}), writes.ErrNoRowsAffected)
	})

	T.Run("nil result", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, writes.RequireAffectedResult(nil), platformerrors.ErrNilInputParameter)
	})

	T.Run("a driver that will not say", func(t *testing.T) {
		t.Parallel()

		unsupported := errors.New("RowsAffected is not supported")

		test.ErrorIs(t, writes.RequireAffectedResult(affectedResult{err: unsupported}), unsupported)
	})
}

// affectedResult is a sql.Result that reports whatever the test needs it to.
type affectedResult struct {
	err      error
	affected int64
}

var _ sql.Result = (*affectedResult)(nil)

func (r affectedResult) LastInsertId() (int64, error) { return 0, r.err }
func (r affectedResult) RowsAffected() (int64, error) { return r.affected, r.err }
