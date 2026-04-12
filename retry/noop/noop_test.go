package noop

import (
	"context"
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestPolicy_Execute(T *testing.T) {
	T.Parallel()

	T.Run("executes exactly once on success", func(t *testing.T) {
		t.Parallel()

		p := NewPolicy()
		ctx := context.Background()
		attempts := 0

		err := p.Execute(ctx, func(ctx context.Context) error {
			attempts++
			return nil
		})

		must.NoError(t, err)
		test.EqOp(t, 1, attempts)
	})

	T.Run("executes exactly once on failure", func(t *testing.T) {
		t.Parallel()

		p := NewPolicy()
		ctx := context.Background()
		attempts := 0
		expectedErr := errors.New("fail")

		err := p.Execute(ctx, func(ctx context.Context) error {
			attempts++
			return expectedErr
		})

		must.Error(t, err)
		test.ErrorIs(t, err, expectedErr)
		test.EqOp(t, 1, attempts)
	})
}
