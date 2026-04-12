package identifiers

import (
	"testing"

	"github.com/rs/xid"
	"github.com/shoenig/test"
)

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		actual := New()
		test.NotEq(t, "", actual)
	})
}

func TestValidate(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		actual := Validate(xid.New().String())
		test.NoError(t, actual)
	})
}
