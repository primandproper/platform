package noop

import (
	"testing"

	"github.com/primandproper/platform-go/v10/secrets"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterSecretSource(T *testing.T) {
	T.Parallel()

	T.Run("both keys resolve to one source", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		RegisterSecretSource(i)

		concrete, err := do.Invoke[*SecretSource](i)
		must.NoError(t, err)
		must.NotNil(t, concrete)

		iface, err := do.Invoke[secrets.SecretSource](i)
		must.NoError(t, err)

		test.EqOp(t, any(concrete), any(iface))
	})
}
