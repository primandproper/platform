package noop

import (
	"testing"

	"github.com/primandproper/platform-go/v10/cache"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterCache(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterCache[string](i)

		c, err := do.Invoke[*Cache[string]](i)
		must.NoError(t, err)
		test.NotNil(t, c)
	})

	T.Run("both keys resolve to one cache", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterCache[string](i)

		concrete, err := do.Invoke[*Cache[string]](i)
		must.NoError(t, err)

		iface, err := do.Invoke[cache.Cache[string]](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})
}
