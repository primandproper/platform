package healthcheck

import (
	"testing"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterRegistry(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()

		RegisterRegistry(i)

		registry, err := do.Invoke[Registry](i)
		must.NoError(t, err)
		test.NotNil(t, registry)
	})
}
