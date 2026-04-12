package cache

import (
	"testing"

	"github.com/shoenig/test/must"
)

func TestErrNotFound(T *testing.T) {
	T.Parallel()

	T.Run("is not nil", func(t *testing.T) {
		t.Parallel()

		must.NotNil(t, ErrNotFound)
		must.EqOp(t, "not found", ErrNotFound.Error())
	})
}
