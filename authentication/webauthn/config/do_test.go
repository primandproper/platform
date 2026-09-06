package webauthncfg

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v14/authentication/webauthn"
	cachecfg "github.com/primandproper/platform-go/v14/cache/config"
	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/observability/logging"
	loggingnoop "github.com/primandproper/platform-go/v14/observability/logging/noop"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestRegisterSessionStore(T *testing.T) {
	T.Parallel()

	// No database.Client is registered anywhere here, which is the whole reason
	// this half can be wired on its own.
	T.Run("builds the cache store with nothing else registered", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, testConfig())

		RegisterSessionStore(i)

		store, err := do.Invoke[webauthn.SessionStore](i)
		must.NoError(t, err)
		test.NotNil(t, store)
	})

	T.Run("a cache it cannot build fails the container", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.Cache = cachecfg.Config{Provider: cachecfg.ProviderRedis}

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, cfg)

		RegisterSessionStore(i)

		store, err := do.Invoke[webauthn.SessionStore](i)
		must.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("a failing observability pillar is an error rather than a noop", func(t *testing.T) {
		t.Parallel()

		errBuild := errors.New("building the logger")

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, testConfig())
		do.Provide(i, func(do.Injector) (logging.Logger, error) { return nil, errBuild })

		RegisterSessionStore(i)

		_, err := do.Invoke[webauthn.SessionStore](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}

func TestRegisterRelyingParty(T *testing.T) {
	T.Parallel()

	// The relying party reads the store the container already holds rather than
	// building a second one, so whichever half registered it is the one in the
	// ceremonies.
	T.Run("reads the registered store", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, testConfig())
		do.ProvideValue(i, loggingnoop.NewLogger())

		RegisterSessionStore(i)
		RegisterRelyingParty(i)

		rp, err := do.Invoke[*webauthn.RelyingParty](i)
		must.NoError(t, err)
		must.NotNil(t, rp)

		creation, err := rp.BeginRegistration(t.Context(), &testUser{})
		must.NoError(t, err)

		// The store in the ceremonies is the registered one: the challenge it
		// just issued is consumable through the value the container hands out.
		store, err := do.Invoke[webauthn.SessionStore](i)
		must.NoError(t, err)

		session, err := store.Consume(t.Context(), creation.Response.Challenge.String())
		must.NoError(t, err)
		test.EqOp(t, "example.com", session.RelyingPartyID)
	})

	T.Run("a container with no registered store does not come up", func(t *testing.T) {
		t.Parallel()

		i := do.New()
		do.ProvideValue[context.Context](i, t.Context())
		do.ProvideValue(i, testConfig())

		RegisterRelyingParty(i)

		rp, err := do.Invoke[*webauthn.RelyingParty](i)
		must.Error(t, err)
		test.Nil(t, rp)
	})
}
