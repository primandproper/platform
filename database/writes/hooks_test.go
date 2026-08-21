package writes_test

import (
	"testing"

	"github.com/primandproper/platform-go/v12/database/writes"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
)

func TestOp_Valid(T *testing.T) {
	T.Parallel()

	T.Run("the three named operations", func(t *testing.T) {
		t.Parallel()

		for _, op := range []writes.Op{writes.OpCreated, writes.OpUpdated, writes.OpArchived} {
			test.True(t, op.Valid())
		}
	})

	T.Run("anything else", func(t *testing.T) {
		t.Parallel()

		for _, op := range []writes.Op{"", "deleted", "CREATED", "upserted"} {
			test.False(t, op.Valid())
		}
	})
}

func TestChange_Validate(T *testing.T) {
	T.Parallel()

	valid := func() *writes.Change {
		return &writes.Change{
			Resource: "widget",
			Table:    "widgets",
			ID:       "widget_1",
			Op:       writes.OpCreated,
			Scope:    tenancy.Global(),
		}
	}

	T.Run("a complete change", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, valid().Validate())
	})

	T.Run("the table and the owner are optional", func(t *testing.T) {
		t.Parallel()

		change := valid()
		change.Table = ""
		change.Owner = ""

		test.NoError(t, change.Validate())
	})

	T.Run("no resource", func(t *testing.T) {
		t.Parallel()

		change := valid()
		change.Resource = ""

		test.ErrorIs(t, change.Validate(), platformerrors.ErrEmptyInputParameter)
	})

	T.Run("no id", func(t *testing.T) {
		t.Parallel()

		change := valid()
		change.ID = ""

		test.ErrorIs(t, change.Validate(), platformerrors.ErrInvalidIDProvided)
	})

	T.Run("an unrecognized operation", func(t *testing.T) {
		t.Parallel()

		change := valid()
		change.Op = "upserted"

		test.ErrorIs(t, change.Validate(), writes.ErrUnknownOp)
	})

	T.Run("an unset scope", func(t *testing.T) {
		t.Parallel()

		// The zero Scope names nobody, and an audit row carrying it would be an
		// audit row in no tenant. Saying tenancy.Global() is how a write that
		// belongs to no tenant says so.
		change := valid()
		change.Scope = tenancy.Scope{}

		test.Error(t, change.Validate())
	})
}
