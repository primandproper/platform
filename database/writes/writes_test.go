package writes_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v12/database"
	"github.com/primandproper/platform-go/v12/database/writes"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("nil client", func(t *testing.T) {
		t.Parallel()

		writer, err := writes.New(nil)
		test.Nil(t, writer)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	T.Run("nil options are skipped", func(t *testing.T) {
		t.Parallel()

		writer, err := writes.New(newClient(t), nil, writes.WithHook(nil))
		must.NoError(t, err)
		must.NotNil(t, writer)
	})
}

func TestWriter_Do(T *testing.T) {
	T.Parallel()

	T.Run("the row and its hook's row commit together", func(t *testing.T) {
		t.Parallel()

		client, writer, seen := newWriter(t, writes.WithHook(auditHook))

		must.NoError(t, writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			if err := insertWidget(ctx, exec, "widget_1", "sprocket"); err != nil {
				return nil, err
			}

			return []writes.Change{{
				Resource: "widget",
				Table:    "widgets",
				ID:       "widget_1",
				Owner:    "user_1",
				Op:       writes.OpCreated,
				Scope:    tenancy.Of("account_1"),
			}}, nil
		}))

		test.EqOp(t, 1, countRows(t, client, "widgets"))
		test.EqOp(t, 1, countRows(t, client, "audit_log"))

		recorded := seen.all()
		must.SliceLen(t, 1, recorded)
		test.EqOp(t, "widget", recorded[0].Resource)
		test.EqOp(t, "widgets", recorded[0].Table)
		test.EqOp(t, "widget_1", recorded[0].ID)
		test.EqOp(t, "user_1", recorded[0].Owner)
		test.EqOp(t, writes.OpCreated, recorded[0].Op)
		test.EqOp(t, "account_1", recorded[0].Scope.Owner())
	})

	T.Run("nil write", func(t *testing.T) {
		t.Parallel()

		_, writer, _ := newWriter(t)

		test.ErrorIs(t, writer.Do(t.Context(), nil), platformerrors.ErrNilInputParameter)
	})

	T.Run("a failing write rolls back and returns its own error", func(t *testing.T) {
		t.Parallel()

		client, writer, seen := newWriter(t, writes.WithHook(auditHook))

		err := writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			if insertErr := insertWidget(ctx, exec, "widget_1", "sprocket"); insertErr != nil {
				return nil, insertErr
			}

			return nil, writes.ErrNoRowsAffected
		})

		// Unwrapped, and therefore still the 404 the service will map it to.
		must.ErrorIs(t, err, writes.ErrNoRowsAffected)
		test.ErrorIs(t, err, sql.ErrNoRows)

		test.EqOp(t, 0, countRows(t, client, "widgets"))
		test.SliceEmpty(t, seen.all())
	})

	T.Run("a failing hook rolls the write back", func(t *testing.T) {
		t.Parallel()

		errHook := errors.New("hook says no")

		client, writer, _ := newWriter(t, writes.WithHook(auditHook),
			writes.WithHook(func(context.Context, database.SQLQueryExecutor, *writes.Change) error { return errHook }))

		err := writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			if insertErr := insertWidget(ctx, exec, "widget_1", "sprocket"); insertErr != nil {
				return nil, insertErr
			}

			return []writes.Change{newChange("widget_1", writes.OpCreated)}, nil
		})

		must.ErrorIs(t, err, errHook)

		// Neither the row nor the entry the earlier hook wrote for it survives.
		test.EqOp(t, 0, countRows(t, client, "widgets"))
		test.EqOp(t, 0, countRows(t, client, "audit_log"))
	})

	T.Run("an unrecordable change rolls the write back before any hook runs", func(t *testing.T) {
		t.Parallel()

		client, writer, seen := newWriter(t)

		err := writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			if insertErr := insertWidget(ctx, exec, "widget_1", "sprocket"); insertErr != nil {
				return nil, insertErr
			}

			return []writes.Change{
				newChange("widget_1", writes.OpCreated),
				{Resource: "widget", Op: writes.OpCreated, Scope: tenancy.Global()},
			}, nil
		})

		must.ErrorIs(t, err, platformerrors.ErrInvalidIDProvided)

		test.EqOp(t, 0, countRows(t, client, "widgets"))
		// The valid change was not reported either: the whole write is one unit.
		test.SliceEmpty(t, seen.all())
	})

	T.Run("changes are validated with no hooks registered", func(t *testing.T) {
		t.Parallel()

		client := newClient(t)

		writer, err := writes.New(client)
		must.NoError(t, err)

		// A writer nobody has attached a hook to still rejects a change nothing
		// could record, so attaching the first hook is not the change that
		// breaks a working application.
		must.ErrorIs(t, writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			if insertErr := insertWidget(ctx, exec, "widget_1", "sprocket"); insertErr != nil {
				return nil, insertErr
			}

			return []writes.Change{{Resource: "widget", ID: "widget_1", Op: writes.OpCreated}}, nil
		}), platformerrors.ErrEmptyInputParameter)

		test.EqOp(t, 0, countRows(t, client, "widgets"))
	})

	T.Run("hooks run in registration order, changes in reported order", func(t *testing.T) {
		t.Parallel()

		var order []string

		note := func(name string) writes.Hook {
			return func(_ context.Context, _ database.SQLQueryExecutor, change *writes.Change) error {
				order = append(order, name+":"+change.ID)

				return nil
			}
		}

		client := newClient(t)

		writer, err := writes.New(client, writes.WithHook(note("first")), writes.WithHook(note("second")))
		must.NoError(t, err)

		must.NoError(t, writer.Do(t.Context(), func(context.Context, database.SQLQueryExecutor) ([]writes.Change, error) {
			return []writes.Change{
				newChange("widget_1", writes.OpArchived),
				newChange("widget_2", writes.OpArchived),
			}, nil
		}))

		test.Eq(t, []string{"first:widget_1", "second:widget_1", "first:widget_2", "second:widget_2"}, order)
	})

	T.Run("a write that reports nothing still commits", func(t *testing.T) {
		t.Parallel()

		client, writer, seen := newWriter(t)

		must.NoError(t, writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			return nil, insertWidget(ctx, exec, "widget_1", "sprocket")
		}))

		test.EqOp(t, 1, countRows(t, client, "widgets"))
		test.SliceEmpty(t, seen.all())
	})

	T.Run("a cascade reports one change per row", func(t *testing.T) {
		t.Parallel()

		client, writer, seen := newWriter(t, writes.WithHook(auditHook))

		must.NoError(t, writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			for _, id := range []string{"widget_1", "widget_2", "widget_3"} {
				if err := insertWidget(ctx, exec, id, "sprocket"); err != nil {
					return nil, err
				}
			}

			return nil, nil
		}))

		must.NoError(t, writer.Do(t.Context(), func(ctx context.Context, exec database.SQLQueryExecutor) ([]writes.Change, error) {
			doomed, err := doomedIDs(ctx, exec)
			if err != nil {
				return nil, err
			}

			result, err := exec.ExecContext(ctx, "UPDATE widgets SET archived_at = CURRENT_TIMESTAMP WHERE archived_at IS NULL")
			if err != nil {
				return nil, err
			}

			if err = writes.RequireAffectedResult(result); err != nil {
				return nil, err
			}

			changes := make([]writes.Change, 0, len(doomed))
			for _, id := range doomed {
				changes = append(changes, newChange(id, writes.OpArchived))
			}

			return changes, nil
		}))

		// One entry per archived row, not one for the statement.
		test.EqOp(t, 3, countRows(t, client, "audit_log"))
		test.SliceLen(t, 3, seen.all())
	})
}

// newChange is a valid Change for a widget, for the tests that are not about
// what makes one valid.
func newChange(id string, op writes.Op) writes.Change {
	return writes.Change{
		Resource: "widget",
		Table:    "widgets",
		ID:       id,
		Op:       op,
		Scope:    tenancy.Global(),
	}
}

// auditHook is the hook this package exists for: an append into a second table,
// through the executor of the transaction that wrote the row.
func auditHook(ctx context.Context, exec database.SQLQueryExecutor, change *writes.Change) error {
	_, err := exec.ExecContext(ctx,
		"INSERT INTO audit_log (id, resource, row_id, operation, scope) VALUES (?, ?, ?, ?, ?)",
		string(change.Op)+"_"+change.ID, change.Resource, change.ID, string(change.Op), change.Scope)

	return err
}

// doomedIDs reads the set a cascade is about to archive, inside the transaction
// that archives it.
func doomedIDs(ctx context.Context, exec database.SQLQueryExecutor) ([]string, error) {
	return database.ScanAll(ctx, exec, "widgets", "SELECT id FROM widgets WHERE archived_at IS NULL ORDER BY id", nil,
		func(scanner database.Scanner) (string, error) {
			var id string

			if err := scanner.Scan(&id); err != nil {
				return "", err
			}

			return id, nil
		})
}
