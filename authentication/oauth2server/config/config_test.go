package oauth2servercfg

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	oauth2database "github.com/primandproper/platform-go/v14/authentication/oauth2server/database"
	"github.com/primandproper/platform-go/v14/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// testAuthenticator stands in for whatever a deployment uses to identify a
// human. What it does is irrelevant here; that it must be supplied is the point.
var testAuthenticator = oauth2server.SubjectAuthenticatorFunc(
	func(context.Context, *http.Request) (*oauth2server.Subject, error) {
		return &oauth2server.Subject{ID: "user_1"}, nil
	})

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in every zero field", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, ProviderDatabase, cfg.Provider)
		test.EqOp(t, oauth2server.DefaultAuthorizationCodeTTL, cfg.AuthorizationCodeTTL)
		test.EqOp(t, oauth2server.DefaultAccessTokenTTL, cfg.AccessTokenTTL)
		test.EqOp(t, oauth2server.DefaultRefreshTokenTTL, cfg.RefreshTokenTTL)
		test.EqOp(t, oauth2server.DefaultClientRegistrationTTL, cfg.ClientRegistrationTTL)
		test.EqOp(t, oauth2server.DefaultSweepInterval, cfg.SweepInterval)
	})

	T.Run("defaults to the durable provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		// The memory provider fails logins behind a load balancer, so it is not
		// something an unset environment should produce.
		test.EqOp(t, ProviderDatabase, cfg.Provider)
	})

	T.Run("leaves what was set alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderMemory, AccessTokenTTL: time.Minute}
		cfg.EnsureDefaults()

		test.EqOp(t, ProviderMemory, cfg.Provider)
		test.EqOp(t, time.Minute, cfg.AccessTokenTTL)
	})

	// The zero is the default and NoSweep is the off-switch, which is only true
	// while this method can tell them apart. Defaulting NoSweep would put the
	// off-switch back out of reach.
	T.Run("leaves NoSweep alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{SweepInterval: NoSweep}
		cfg.EnsureDefaults()

		test.EqOp(t, NoSweep, cfg.SweepInterval)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a defaulted config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example"}
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("refuses a provider nobody implements", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: "redis"}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("accepts NoSweep by rule rather than by omission", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", SweepInterval: NoSweep}
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// Below zero there is no cadence to configure — every negative duration
	// reaches the store as "start nothing" — so a magnitude is somebody
	// describing a sweep they will not get.
	T.Run("refuses a negative interval that is not NoSweep", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", SweepInterval: -30 * time.Minute}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("skips the database rules under the memory provider", func(t *testing.T) {
		t.Parallel()

		// `env:",init"` leaves the sub-config populated whichever provider is
		// selected, and ozzo validates any non-nil Validatable once a field's
		// rules run — so without the Skip a perfectly good memory configuration
		// would be unloadable because of a table prefix nothing will use.
		cfg := &Config{Provider: ProviderMemory, Database: databaseConfigWithBadPrefix()}
		test.NoError(t, cfg.ValidateWithContext(t.Context()))

		cfg.Provider = ProviderDatabase
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("refuses a negative lifetime", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderMemory, AccessTokenTTL: -time.Minute}
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewStore(T *testing.T) {
	T.Parallel()

	T.Run("builds the memory provider", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), &Config{Provider: ProviderMemory}, nil)
		must.NoError(t, err)
		must.NotNil(t, store)
		test.NoError(t, store.Close())
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), nil, nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
		test.Nil(t, store)
	})

	T.Run("an unknown provider is an error rather than a working-looking default", func(t *testing.T) {
		t.Parallel()

		// Falling back to memory would produce an authorization server that
		// signs users in, fails their next login on another replica, and looks
		// configured the whole time.
		store, err := NewStore(t.Context(), &Config{Provider: "redis"}, nil)
		test.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("the database provider needs a client", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), &Config{Provider: ProviderDatabase}, nil)
		test.Error(t, err)

		// The nil check is the point. A concrete-typed constructor's result
		// returned straight through would arrive here as a non-nil interface
		// wrapping a nil pointer — a value that passes this check and panics on
		// first use.
		test.Nil(t, store)
	})
}

// TestConfig_SweepInterval exercises the field's documented contract where it
// is actually decided. The stores' own WithSweeper has always honored a
// non-positive interval; what could not reach it was a configured one, because
// EnsureDefaults mapped every zero to the default before the option was built.
//
// The observable is Store.Sweep's own count. A record the background sweeper
// already removed leaves the scheduled sweep nothing to do, which is exactly
// the duplication a deployment turns the sweeper off to avoid.
func TestConfig_SweepInterval(T *testing.T) {
	T.Parallel()

	// The wall clock is deliberate: inside a synctest bubble clock.NewClock
	// reads the bubble's time, so the store's ticker advances with time.Sleep
	// and needs no test double.
	T.Run("NoSweep leaves dead records for somebody else to remove", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			store, err := NewStore(t.Context(),
				&Config{Provider: ProviderMemory, SweepInterval: NoSweep}, nil)
			must.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			must.NoError(t, store.CreateAuthorizationCode(t.Context(), expiringCode()))

			// Past the code's deadline and past every tick a defaulted interval
			// would have taken.
			time.Sleep(time.Hour)
			synctest.Wait()

			// The scheduled sweep this deployment runs instead still has work,
			// which is only true because nothing swept behind its back.
			swept, err := store.Sweep(t.Context(), time.Now())
			must.NoError(t, err)
			test.EqOp(t, int64(1), swept)
		})
	})

	T.Run("an unset interval sweeps, because a table nobody sweeps only grows", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			store, err := NewStore(t.Context(), &Config{Provider: ProviderMemory}, nil)
			must.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			must.NoError(t, store.CreateAuthorizationCode(t.Context(), expiringCode()))

			time.Sleep(time.Hour)
			synctest.Wait()

			swept, err := store.Sweep(t.Context(), time.Now())
			must.NoError(t, err)
			test.EqOp(t, int64(0), swept)
		})
	})
}

// expiringCode is one authorization code, redeemable for a minute — long
// enough to be alive when it is written and dead long before either test looks
// again.
func expiringCode() *oauth2server.AuthorizationCode {
	now := time.Now().UTC()

	return &oauth2server.AuthorizationCode{
		IssuedAt:  now,
		ExpiresAt: now.Add(time.Minute),
		Hash:      oauth2server.Hash("swept"),
		ClientID:  "client",
		Subject:   oauth2server.Subject{ID: "user"},
	}
}

func TestNewServer_StoreFailure(T *testing.T) {
	T.Parallel()

	T.Run("a store that cannot be built is reported rather than skipped", func(t *testing.T) {
		t.Parallel()

		// NewServer builds the store first, and a server over a store that
		// failed to build would be an authorization server with nowhere to keep
		// an authorization code.
		srv, err := NewServer(t.Context(),
			&Config{Provider: ProviderDatabase, Issuer: "https://auth.example"},
			nil, testAuthenticator)

		test.Error(t, err)
		test.Nil(t, srv)
	})
}

func TestNewServer(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderMemory, Issuer: "https://auth.example", Scopes: []string{"read"}}

		srv, err := NewServer(t.Context(), cfg, nil, testAuthenticator)
		must.NoError(t, err)
		must.NotNil(t, srv)

		test.EqOp(t, "https://auth.example", srv.Issuer())
		test.Eq(t, []string{"read"}, srv.Metadata().ScopesSupported)
	})

	T.Run("carries the configured lifetimes through", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider:       ProviderMemory,
			Issuer:         "https://auth.example",
			AccessTokenTTL: 42 * time.Second,
		}

		srv, err := NewServer(t.Context(), cfg, nil, testAuthenticator)
		must.NoError(t, err)
		test.NotNil(t, srv)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		srv, err := NewServer(t.Context(), nil, nil, testAuthenticator)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
		test.Nil(t, srv)
	})

	T.Run("an issuer that is not one fails at construction", func(t *testing.T) {
		t.Parallel()

		// Not re-validated by this Config, deliberately: the rule lives in
		// oauth2server.NewServer, and a second copy would be a second place for
		// it to be wrong.
		srv, err := NewServer(t.Context(), &Config{Provider: ProviderMemory}, nil, testAuthenticator)
		test.ErrorIs(t, err, oauth2server.ErrEmptyIssuer)
		test.Nil(t, srv)
	})

	T.Run("carries the registration switch through", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Provider:                   ProviderMemory,
			Issuer:                     "https://auth.example",
			DisableDynamicRegistration: true,
		}

		srv, err := NewServer(t.Context(), cfg, nil, testAuthenticator)
		must.NoError(t, err)

		// The discovery document is where the switch is visible, and the
		// endpoint being absent from it is the point: a deployment whose
		// clients are administered elsewhere publishes no /register.
		test.EqOp(t, "", srv.Metadata().RegistrationEndpoint)
	})

	T.Run("serves registration when nothing turned it off", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderMemory, Issuer: "https://auth.example"}

		srv, err := NewServer(t.Context(), cfg, nil, testAuthenticator)
		must.NoError(t, err)

		// Spelled as a disable so that an unset environment is the protocol's
		// own behavior rather than a server a client cannot register with.
		test.EqOp(t, "https://auth.example"+oauth2server.PathRegister, srv.Metadata().RegistrationEndpoint)
	})

	T.Run("options passed through win over the config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderMemory, Issuer: "https://auth.example", Scopes: []string{"read"}}

		srv, err := NewServer(t.Context(), cfg, nil, testAuthenticator,
			WithServerOptions(oauth2server.WithServiceDocumentation("https://docs.example")))
		must.NoError(t, err)

		test.EqOp(t, "https://docs.example", srv.Metadata().ServiceDocumentation)
	})
}

// databaseConfigWithBadPrefix is a database sub-config that would fail its own
// validation, for the case that proves the memory provider does not run it.
func databaseConfigWithBadPrefix() oauth2database.Config {
	return oauth2database.Config{TablePrefix: "trailing_"}
}
