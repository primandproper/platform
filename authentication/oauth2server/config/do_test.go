package oauth2servercfg

import (
	"context"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/observability/logging"

	"github.com/samber/do/v2"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// newInjector registers the prerequisites both registrations share.
func newInjector(t *testing.T, cfg *Config) do.Injector {
	t.Helper()

	i := do.New()
	do.ProvideValue[context.Context](i, t.Context())
	do.ProvideValue(i, cfg)

	return i
}

func TestRegisterStore(T *testing.T) {
	T.Parallel()

	// No database.Client is registered anywhere here, which is the whole reason
	// this half can be wired on its own.
	T.Run("builds the memory store with nothing else registered", func(t *testing.T) {
		t.Parallel()

		i := newInjector(t, &Config{})
		RegisterStore(i)

		store, err := do.Invoke[oauth2server.Store](i)
		must.NoError(t, err)
		test.NotNil(t, store)
	})

	T.Run("a config that cannot validate fails the container", func(t *testing.T) {
		t.Parallel()

		i := newInjector(t, &Config{AccessTokenTTL: -time.Minute})
		RegisterStore(i)

		store, err := do.Invoke[oauth2server.Store](i)
		must.Error(t, err)
		test.Nil(t, store)
	})

	// Absent observability is fine; observability that was registered and
	// cannot be built is an error rather than a silent noop.
	T.Run("a failing observability pillar is an error rather than a noop", func(t *testing.T) {
		t.Parallel()

		errBuild := platformerrors.New("building the logger")

		i := newInjector(t, &Config{})
		do.Provide(i, func(do.Injector) (logging.Logger, error) { return nil, errBuild })
		RegisterStore(i)

		_, err := do.Invoke[oauth2server.Store](i)
		must.Error(t, err)
		test.ErrorIs(t, err, errBuild)
	})
}

func TestRegisterServer(T *testing.T) {
	T.Parallel()

	// The server reads the store the container already holds rather than
	// building a second one, so whichever half registered it is the one the
	// server issues codes against.
	T.Run("reads the registered store", func(t *testing.T) {
		t.Parallel()

		i := newInjector(t, &Config{Issuer: "https://auth.example"})
		do.ProvideValue[oauth2server.SubjectAuthenticator](i, testAuthenticator)

		RegisterStore(i)
		RegisterServer(i)

		srv, err := do.Invoke[*oauth2server.Server](i)
		must.NoError(t, err)
		must.NotNil(t, srv)

		test.EqOp(t, "https://auth.example", srv.Issuer())
	})

	T.Run("a container with no registered store does not come up", func(t *testing.T) {
		t.Parallel()

		i := newInjector(t, &Config{Issuer: "https://auth.example"})
		do.ProvideValue[oauth2server.SubjectAuthenticator](i, testAuthenticator)

		RegisterServer(i)

		srv, err := do.Invoke[*oauth2server.Server](i)
		must.Error(t, err)
		test.Nil(t, srv)
	})

	// The authenticator is how this deployment identifies a human, so a
	// container that registered none fails rather than coming up with something
	// that cannot authenticate anybody.
	T.Run("a container with no authenticator does not come up", func(t *testing.T) {
		t.Parallel()

		i := newInjector(t, &Config{Issuer: "https://auth.example"})

		RegisterStore(i)
		RegisterServer(i)

		srv, err := do.Invoke[*oauth2server.Server](i)
		test.Error(t, err)
		test.Nil(t, srv)
	})
}
