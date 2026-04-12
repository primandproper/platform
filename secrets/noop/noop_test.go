package noop

import (
	"context"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestSecretSource_GetSecret(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		source := NewSecretSource()
		ctx := context.Background()

		got, err := source.GetSecret(ctx, "any-key")
		must.NoError(t, err)
		test.EqOp(t, "", got)
	})
}

func TestSecretSource_Close(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		source := NewSecretSource()
		err := source.Close()
		must.NoError(t, err)
	})
}
