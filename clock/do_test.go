package clock

import (
	"testing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterClock(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()

		RegisterClock(i)

		c, err := do.Invoke[Clock](i)
		must.NoError(t, err)
		test.NotNil(t, c)
	})
}
