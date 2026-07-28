package database

import (
	"context"
	"database/sql"

	"github.com/primandproper/platform-go/v8/database"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
)

// scanRows drives a result set through scan, closing it afterwards. A close
// failure is surfaced only when nothing worse already went wrong, so the real
// cause is never masked by the cleanup.
func scanRows(rows *sql.Rows, scan func() error) (err error) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = platformerrors.Wrap(closeErr, "closing authorization rows")
		}
	}()

	for rows.Next() {
		if err = scan(); err != nil {
			return err
		}
	}

	return rows.Err()
}

// scanStrings runs a single-column query and collects the results.
func scanStrings(ctx context.Context, q database.SQLQueryExecutor, query string, args []any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	var out []string
	if err = scanRows(rows, func() error {
		var value string
		if scanErr := rows.Scan(&value); scanErr != nil {
			return scanErr
		}
		out = append(out, value)

		return nil
	}); err != nil {
		return nil, err
	}

	return out, nil
}
