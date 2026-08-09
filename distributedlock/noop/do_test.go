package noop

import (
	"testing"

	"github.com/primandproper/platform-go/v10/distributedlock"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterLocker(T *testing.T) {
	T.Parallel()

	T.Run("both keys resolve to one locker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterLocker(i)

		concrete, err := do.Invoke[*Locker](i)
		must.NoError(t, err)
		must.NotNil(t, concrete)

		iface, err := do.Invoke[distributedlock.Locker](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})
}

func TestRegisterScopedLocker(T *testing.T) {
	T.Parallel()

	T.Run("both keys resolve to one locker", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterScopedLocker(i)

		concrete, err := do.Invoke[*ScopedLocker](i)
		must.NoError(t, err)
		must.NotNil(t, concrete)

		iface, err := do.Invoke[distributedlock.ScopedLocker](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})
}
