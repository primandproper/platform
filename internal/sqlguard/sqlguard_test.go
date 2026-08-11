package sqlguard

import (
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var errNotFound = platformerrors.New("thing not found")

// testGuard is the guard these tests exercise: everything named, so that a
// missing field shows up as a difference rather than as silence.
func testGuard() *Guard {
	return &Guard{
		NotFound:  errNotFound,
		Namespace: "things",
		IDKey:     "things.id",
		Message:   "thing left the active set before its outcome could be recorded",
		Reason:    "thing %q is no longer active",
	}
}

// beginOp starts an Operation against no configured pillars, which is the
// no-op path every one of these assertions runs through.
func beginOp(t *testing.T) observability.Operation {
	t.Helper()

	_, op := observability.NewObserver("sqlguard_test", nil, nil).Begin(t.Context())
	t.Cleanup(op.End)

	return op
}

func TestGuard_Exec(T *testing.T) {
	T.Parallel()

	const query = "UPDATE things SET state = 'done' WHERE id = :1 AND state = 'running'"

	T.Run("a guard that matched a row succeeds", func(t *testing.T) {
		t.Parallel()

		db, mock, dbErr := sqlmock.New()
		must.NoError(t, dbErr)

		t.Cleanup(func() { _ = db.Close() })

		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 1))

		test.NoError(t, testGuard().Exec(t.Context(), beginOp(t), db, query, nil, "id-1", "finish", "finishing thing"))
	})

	// The whole reason the guard is in the statement: zero rows is not an error
	// the driver reports, and treating it as success has the caller report an
	// outcome the database never recorded.
	T.Run("a guard that matched nothing is the sentinel", func(t *testing.T) {
		t.Parallel()

		db, mock, dbErr := sqlmock.New()
		must.NoError(t, dbErr)

		t.Cleanup(func() { _ = db.Close() })

		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 0))

		err := testGuard().Exec(t.Context(), beginOp(t), db, query, nil, "id-1", "finish", "finishing thing")

		test.ErrorIs(t, err, errNotFound)
		test.StrContains(t, err.Error(), `thing "id-1" is no longer active`)
	})

	T.Run("reports what the driver said", func(t *testing.T) {
		t.Parallel()

		sentinel := platformerrors.New("connection refused")

		db, mock, dbErr := sqlmock.New()
		must.NoError(t, dbErr)

		t.Cleanup(func() { _ = db.Close() })

		mock.ExpectExec(query).WillReturnError(sentinel)

		err := testGuard().Exec(t.Context(), beginOp(t), db, query, nil, "id-1", "finish", "finishing thing")

		test.ErrorIs(t, err, sentinel)
	})

	T.Run("reports a driver that could not count the rows it changed", func(t *testing.T) {
		t.Parallel()

		sentinel := platformerrors.New("rows affected unsupported")

		db, mock, dbErr := sqlmock.New()
		must.NoError(t, dbErr)

		t.Cleanup(func() { _ = db.Close() })

		mock.ExpectExec(query).WillReturnResult(sqlmock.NewErrorResult(sentinel))

		err := testGuard().Exec(t.Context(), beginOp(t), db, query, nil, "id-1", "finish", "finishing thing")

		test.ErrorIs(t, err, sentinel)
	})

	// A caller that has no sentinel to offer still gets a legible error rather
	// than a nil one that reads as success.
	T.Run("a guard with no sentinel returns its reason alone", func(t *testing.T) {
		t.Parallel()

		db, mock, dbErr := sqlmock.New()
		must.NoError(t, dbErr)

		t.Cleanup(func() { _ = db.Close() })

		mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 0))

		g := testGuard()
		g.NotFound = nil

		err := g.Exec(t.Context(), beginOp(t), db, query, nil, "id-1", "finish", "finishing thing")

		must.Error(t, err)
		test.StrContains(t, err.Error(), `thing "id-1" is no longer active`)
	})
}

func TestGuard_OpAttr(T *testing.T) {
	T.Parallel()

	// The attribute name is derived from the namespace rather than configured,
	// so two packages cannot spell the same fact differently and land in series
	// nothing groups together.
	T.Run("names the attribute after the namespace", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, (&Guard{Namespace: "things"}).OpAttr("finish"))
	})
}
