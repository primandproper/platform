package oauth2servercfg

import (
	"context"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	oauth2memory "github.com/primandproper/platform-go/v14/authentication/oauth2server/memory"
	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/pointer"

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

	T.Run("fills in the lifetimes and the sweep", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, oauth2server.DefaultAuthorizationCodeTTL, cfg.AuthorizationCodeTTL)
		test.EqOp(t, oauth2server.DefaultAccessTokenTTL, cfg.AccessTokenTTL)
		test.EqOp(t, oauth2server.DefaultRefreshTokenTTL, cfg.RefreshTokenTTL)
		test.EqOp(t, oauth2server.DefaultClientRegistrationTTL, pointer.Dereference(cfg.ClientRegistrationTTL))
		test.EqOp(t, oauth2server.DefaultSweepInterval, pointer.Dereference(cfg.SweepInterval))
	})

	T.Run("leaves configured values alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{AccessTokenTTL: time.Minute}
		cfg.EnsureDefaults()

		test.EqOp(t, time.Minute, cfg.AccessTokenTTL)
	})

	// Nil is the default and a spelled zero is the off-switch, which is only
	// true while this method can tell them apart.
	T.Run("leaves a spelled zero alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			SweepInterval:         pointer.To(time.Duration(0)),
			ClientRegistrationTTL: pointer.To(time.Duration(0)),
		}
		cfg.EnsureDefaults()

		test.EqOp(t, time.Duration(0), pointer.Dereference(cfg.SweepInterval))
		test.EqOp(t, time.Duration(0), pointer.Dereference(cfg.ClientRegistrationTTL))
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("the zero value is valid", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, (&Config{}).ValidateWithContext(t.Context()))
	})

	T.Run("accepts the off-switch by rule rather than by omission", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", SweepInterval: pointer.To(time.Duration(0))}

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// Below zero there is no cadence to configure — every negative duration
	// reaches the store as "start nothing" — so a magnitude is somebody
	// describing a sweep they will not get.
	T.Run("rejects a negative interval", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", SweepInterval: pointer.To(-30 * time.Minute)}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects a negative lifetime", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{AccessTokenTTL: -time.Minute}).ValidateWithContext(t.Context()))
	})

	T.Run("rejects a negative registration lifetime", func(t *testing.T) {
		t.Parallel()

		test.Error(t, (&Config{ClientRegistrationTTL: pointer.To(-time.Hour)}).ValidateWithContext(t.Context()))
	})
}

func TestNewStore(T *testing.T) {
	T.Parallel()

	T.Run("builds the memory store", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), &Config{})
		must.NoError(t, err)
		must.NotNil(t, store)

		_, ok := store.(*oauth2memory.Store)
		test.True(t, ok)
	})

	T.Run("refuses a nil config", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
		test.Nil(t, store)
	})

	T.Run("refuses a config that cannot validate", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), &Config{AccessTokenTTL: -time.Minute})
		test.Error(t, err)
		test.Nil(t, store)
	})

	T.Run("passes store options through", func(t *testing.T) {
		t.Parallel()

		store, err := NewStore(t.Context(), &Config{},
			WithMemoryStoreOptions(oauth2memory.WithLogger(nil)))

		must.NoError(t, err)
		test.NotNil(t, store)
	})
}

// The store this half builds sweeps too, which is why SweepInterval stayed here
// rather than travelling with the provider string.
func TestConfig_SweepInterval(T *testing.T) {
	T.Parallel()

	T.Run("a zero interval starts no sweeper", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			store, err := NewStore(t.Context(), &Config{SweepInterval: pointer.To(time.Duration(0))})
			must.NoError(t, err)

			must.NoError(t, store.CreateAuthorizationCode(t.Context(), &oauth2server.AuthorizationCode{
				IssuedAt:  time.Now().UTC(),
				ExpiresAt: time.Now().UTC().Add(time.Minute),
				Hash:      oauth2server.Hash("abandoned"),
				ClientID:  "client",
			}))

			time.Sleep(time.Hour)
			synctest.Wait()

			// Still there, expired: Consume refuses it because it is past its
			// deadline, not because the record is gone.
			_, err = store.ConsumeAuthorizationCode(t.Context(), oauth2server.Hash("abandoned"))
			test.ErrorIs(t, err, oauth2server.ErrExpired)
		})
	})

	T.Run("an unset interval sweeps", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			store, err := NewStore(t.Context(), &Config{})
			must.NoError(t, err)

			must.NoError(t, store.CreateAuthorizationCode(t.Context(), &oauth2server.AuthorizationCode{
				IssuedAt:  time.Now().UTC(),
				ExpiresAt: time.Now().UTC().Add(time.Minute),
				Hash:      oauth2server.Hash("abandoned"),
				ClientID:  "client",
			}))

			time.Sleep(time.Hour)
			synctest.Wait()

			// Gone rather than expired: the sweeper removed the record.
			_, err = store.ConsumeAuthorizationCode(t.Context(), oauth2server.Hash("abandoned"))
			test.ErrorIs(t, err, oauth2server.ErrNotFound)
		})
	})
}

func TestNewServer(T *testing.T) {
	T.Parallel()

	newStore := func(t *testing.T) oauth2server.Store {
		t.Helper()

		store, err := NewStore(t.Context(), &Config{})
		must.NoError(t, err)

		return store
	}

	T.Run("builds a server over the store it is handed", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", Scopes: []string{"read"}}

		srv, err := NewServer(t.Context(), cfg, newStore(t), testAuthenticator)
		must.NoError(t, err)
		must.NotNil(t, srv)

		test.EqOp(t, "https://auth.example", srv.Issuer())
		test.Eq(t, []string{"read"}, srv.Metadata().ScopesSupported)
	})

	T.Run("carries the registration switch through", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", DisableDynamicRegistration: true}

		srv, err := NewServer(t.Context(), cfg, newStore(t), testAuthenticator)
		must.NoError(t, err)

		test.EqOp(t, "", srv.Metadata().RegistrationEndpoint)
	})

	T.Run("refuses a nil config", func(t *testing.T) {
		t.Parallel()

		srv, err := NewServer(t.Context(), nil, nil, testAuthenticator)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
		test.Nil(t, srv)
	})

	T.Run("refuses an issuer the protocol does not allow", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "http://auth.example"}

		srv, err := NewServer(t.Context(), cfg, newStore(t), testAuthenticator)
		test.Error(t, err)
		test.Nil(t, srv)
	})

	T.Run("passed-through options win over the config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Issuer: "https://auth.example", Scopes: []string{"read"}}

		srv, err := NewServer(t.Context(), cfg, newStore(t), testAuthenticator,
			WithServerOptions(oauth2server.WithServiceDocumentation("https://docs.example")))
		must.NoError(t, err)

		test.EqOp(t, "https://docs.example", srv.Metadata().ServiceDocumentation)
	})
}
