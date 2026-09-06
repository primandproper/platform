package webauthncfg

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/authentication/webauthn"
	webauthncache "github.com/primandproper/platform-go/v14/authentication/webauthn/cache"
	cachecfg "github.com/primandproper/platform-go/v14/cache/config"
	"github.com/primandproper/platform-go/v14/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// relyingParty is the one part of a Config that is always required.
func relyingParty() webauthn.Config {
	return webauthn.Config{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
	}
}

// testConfig is backed by the memory cache, which is for tests and
// single-process services only — see the package documentation on why a fleet
// wants redis.
func testConfig() *Config {
	return &Config{
		RelyingParty: relyingParty(),
		Cache:        cachecfg.Config{Provider: cachecfg.ProviderMemory},
	}
}

// testSession is one ceremony's worth of state, for the assembled store to
// hold.
func testSession(challenge string) *webauthn.SessionData {
	return &webauthn.SessionData{
		Challenge:      challenge,
		RelyingPartyID: "example.com",
		UserID:         []byte("user-handle"),
		Expires:        time.Now().UTC().Add(time.Minute),
	}
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in the relying party's own", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, webauthn.DefaultCeremonyTimeout, cfg.RelyingParty.CeremonyTimeout)
	})

	T.Run("leaves configured values alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.RelyingParty.CeremonyTimeout = 30 * time.Second
		cfg.EnsureDefaults()

		test.EqOp(t, 30*time.Second, cfg.RelyingParty.CeremonyTimeout)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a configured cache and relying party", func(t *testing.T) {
		t.Parallel()

		must.NoError(t, testConfig().ValidateWithContext(t.Context()))
	})

	T.Run("rejects a relying party no ceremony could run under", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.RelyingParty.RPOrigins = nil

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	// The cache is the only store this half builds, so unlike the provider-aware
	// half there is nothing to skip: its rules always apply.
	T.Run("rejects a cache it does not implement", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.Cache = cachecfg.Config{Provider: "a-filing-cabinet"}

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewSessionStore(T *testing.T) {
	T.Parallel()

	T.Run("builds the cache store", func(t *testing.T) {
		t.Parallel()

		store, err := NewSessionStore(t.Context(), testConfig())
		must.NoError(t, err)

		_, ok := store.(*webauthncache.SessionStore)
		test.True(t, ok)

		// It is a store that works, not merely one that was constructed.
		must.NoError(t, store.Save(t.Context(), testSession("built"), time.Minute))

		session, err := store.Consume(t.Context(), "built")
		must.NoError(t, err)
		test.EqOp(t, "built", session.Challenge)
	})

	T.Run("refuses a nil config", func(t *testing.T) {
		t.Parallel()

		store, err := NewSessionStore(t.Context(), nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
		test.Nil(t, store)
	})

	T.Run("reports a cache it cannot build", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.Cache = cachecfg.Config{Provider: cachecfg.ProviderRedis}

		store, err := NewSessionStore(t.Context(), cfg)
		test.Error(t, err)

		// Nil as an interface, not a non-nil interface holding a nil pointer:
		// the concrete constructor returns its own type, and returning one
		// straight through on the error path would hand back a value that
		// passes a nil check and panics on first use.
		test.Nil(t, store)
	})

	T.Run("passes store options through", func(t *testing.T) {
		t.Parallel()

		store, err := NewSessionStore(t.Context(), testConfig(),
			WithCacheStoreOptions(webauthncache.WithLogger(nil)))

		must.NoError(t, err)
		test.NotNil(t, store)
	})
}

func TestNewRelyingParty(T *testing.T) {
	T.Parallel()

	T.Run("builds a relying party over the store it is handed", func(t *testing.T) {
		t.Parallel()

		store, err := NewSessionStore(t.Context(), testConfig())
		must.NoError(t, err)

		rp, err := NewRelyingParty(t.Context(), testConfig(), store)
		must.NoError(t, err)
		must.NotNil(t, rp)

		// A ceremony begins, which means the relying party and the store under
		// it were wired to each other rather than merely built.
		creation, err := rp.BeginRegistration(t.Context(), &testUser{})
		must.NoError(t, err)
		test.EqOp(t, "example.com", creation.Response.RelyingParty.ID)
	})

	// The store is somebody else's to configure — under webauthndbcfg it is a
	// SQL table, built from a block this Config does not have — so a cache
	// nobody asked for is not this function's business.
	T.Run("ignores the cache block, having been handed a store", func(t *testing.T) {
		t.Parallel()

		store, err := NewSessionStore(t.Context(), testConfig())
		must.NoError(t, err)

		cfg := &Config{RelyingParty: relyingParty()}

		rp, err := NewRelyingParty(t.Context(), cfg, store)
		must.NoError(t, err)
		test.NotNil(t, rp)
	})

	T.Run("refuses a nil config", func(t *testing.T) {
		t.Parallel()

		rp, err := NewRelyingParty(t.Context(), nil, nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
		test.Nil(t, rp)
	})

	T.Run("refuses a relying party no ceremony could run under", func(t *testing.T) {
		t.Parallel()

		cfg := testConfig()
		cfg.RelyingParty.RPID = ""

		rp, err := NewRelyingParty(t.Context(), cfg, nil)
		test.Error(t, err)
		test.Nil(t, rp)
	})
}

// testUser is the adapter an application writes, minimal enough to begin a
// ceremony.
type testUser struct{}

var _ webauthn.User = (*testUser)(nil)

func (*testUser) WebAuthnID() []byte                         { return []byte("user-handle") }
func (*testUser) WebAuthnName() string                       { return "user" }
func (*testUser) WebAuthnDisplayName() string                { return "User" }
func (*testUser) WebAuthnCredentials() []webauthn.Credential { return nil }
