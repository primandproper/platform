package writes

import (
	"database/sql"

	platformerrors "github.com/primandproper/platform-go/v12/errors"
)

// ErrNoRowsAffected indicates a write whose statement matched nothing: the row
// is gone, or it is not this owner's, or it is not in this scope.
//
// It wraps sql.ErrNoRows deliberately, so errors.Is(err, sql.ErrNoRows) holds.
// Services map that to a 404 through errors/http and errors/grpc, and a fresh
// sentinel that did not wrap it would quietly turn every missed write in an
// application into a 500 until both mappers learned about it.
//
// The three causes are one error on purpose. Distinguishing "no such row" from
// "not yours" tells an unauthorized caller which ids exist, which is the
// enumeration the owner predicate was in the statement to prevent.
var ErrNoRowsAffected = platformerrors.Wrap(sql.ErrNoRows, "no rows affected")

// RequireAffected reports a statement that matched no rows.
//
// It is here because the check is a convention rather than a line of logic:
// almost every update and archive in a repository ends with it, the sentinel it
// returns decides what status code the caller's service produces, and a copy of
// it that returned something else would be a 500 on a path that means 404. What
// the caller passes is whatever its generated querier handed back — sqlc's
// :execrows returns exactly this int64.
//
// A negative count is not a miss. Drivers that cannot report an affected count
// return -1, and treating "unknown" as "nothing" would fail writes that
// succeeded.
func RequireAffected(affected int64) error {
	if affected == 0 {
		return ErrNoRowsAffected
	}

	return nil
}

// RequireAffectedResult is RequireAffected for a caller holding a driver result
// rather than a count — a hand-rolled ExecContext instead of a generated
// :execrows.
func RequireAffectedResult(result sql.Result) error {
	if result == nil {
		return platformerrors.ErrNilInputParameter
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return platformerrors.Wrap(err, "reading rows affected")
	}

	return RequireAffected(affected)
}
