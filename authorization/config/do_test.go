package authorizationcfg

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v14/authorization"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterPolicyResolver(T *testing.T) {
	T.Parallel()

	T.Run("builds the static resolver without a database.Client or cache", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, &Config{})

		RegisterPolicyResolver(i)

		resolver, err := do.Invoke[authorization.PolicyResolver](i)
		must.NoError(t, err)
		test.NotNil(t, resolver)
	})

	// The registration resolves no database.Client, which is the whole reason a
	// container running declared policy can be built from this half alone.
	T.Run("a config that cannot validate fails the container", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, &Config{CacheTTL: -1})

		RegisterPolicyResolver(i)

		resolver, err := do.Invoke[authorization.PolicyResolver](i)
		must.Error(t, err)
		test.Nil(t, resolver)
	})
}
