package profilingcfg

import (
	"context"
	"testing"

	"github.com/primandproper/platform/observability/logging"
	"github.com/primandproper/platform/observability/profiling"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterProfilingProvider(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, logging.NewNoopLogger())
		do.ProvideValue(i, &Config{})

		RegisterProfilingProvider(i)

		p, err := do.Invoke[profiling.Provider](i)
		must.NoError(t, err)
		test.NotNil(t, p)
	})
}
