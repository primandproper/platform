package linkscfg

import (
	"testing"
	"time"

	cachecfg "github.com/primandproper/platform-go/v14/cache/config"
	distributedlockcfg "github.com/primandproper/platform-go/v14/distributedlock/config"
	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/links"
	linkscache "github.com/primandproper/platform-go/v14/links/cache"
	"github.com/primandproper/platform-go/v14/pointer"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// memoryConfig is a valid in-process configuration, for tests that care about
// everything except the providers.
func memoryConfig() *Config {
	return &Config{
		Provider: ProviderCache,
		Actions: map[links.Action]links.ActionPolicy{
			"magic_login": {URL: "https://app.example.com/auth/magic/{token}", TTL: 15 * time.Minute},
		},
		Cache: cachecfg.Config{Provider: cachecfg.ProviderMemory},
		Lock:  distributedlockcfg.Config{Provider: distributedlockcfg.MemoryProvider},
	}
}

// databaseConfig is the same configuration against the durable store, which
// needs neither a cache nor a locker.
func databaseConfig() *Config {
	cfg := memoryConfig()
	cfg.Provider = ProviderDatabase
	cfg.SweepInterval = pointer.To(time.Duration(0))

	return cfg
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.EqOp(t, links.DefaultRetention, cfg.Retention)
		test.EqOp(t, links.DefaultTokenBytes, cfg.TokenBytes)
		test.EqOp(t, links.DefaultMaxTokenLength, cfg.MaxTokenLength)
		test.EqOp(t, linkscache.DefaultKeyPrefix, cfg.KeyPrefix)
		test.EqOp(t, ProviderCache, cfg.Provider)

		// The cache provider reclaims its own entries, so there is nothing for
		// a sweeper to do and none is started.
		test.Nil(t, cfg.SweepInterval)
	})

	T.Run("defaults the sweep interval only for the database provider", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Provider: ProviderDatabase}
		cfg.EnsureDefaults()

		must.NotNil(t, cfg.SweepInterval)
		test.EqOp(t, DefaultSweepInterval, *cfg.SweepInterval)
	})

	T.Run("a zero sweep interval is a deployment asking for no sweeper", func(t *testing.T) {
		t.Parallel()

		// Unset and zero are different answers, which is the whole reason the
		// field is a pointer: a scheduler calling Sweep for the fleet wants no
		// per-replica sweeper at all.
		cfg := &Config{Provider: ProviderDatabase, SweepInterval: pointer.To(time.Duration(0))}
		cfg.EnsureDefaults()

		must.NotNil(t, cfg.SweepInterval)
		test.EqOp(t, time.Duration(0), *cfg.SweepInterval)
	})

	T.Run("leaves an action's TTL alone", func(t *testing.T) {
		t.Parallel()

		// The absence of a default lifetime is the point: filling one in here
		// would put back exactly what links.ActionPolicy refuses to guess.
		cfg := &Config{Actions: map[links.Action]links.ActionPolicy{"magic_login": {}}}
		cfg.EnsureDefaults()

		test.EqOp(t, time.Duration(0), cfg.Actions["magic_login"].TTL)
	})

	T.Run("leaves set fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Retention:      time.Hour,
			TokenBytes:     64,
			MaxTokenLength: 128,
			KeyPrefix:      "custom:",
		}
		cfg.EnsureDefaults()

		test.EqOp(t, time.Hour, cfg.Retention)
		test.EqOp(t, 64, cfg.TokenBytes)
		test.EqOp(t, 128, cfg.MaxTokenLength)
		test.EqOp(t, "custom:", cfg.KeyPrefix)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a valid config", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, memoryConfig().ValidateWithContext(t.Context()))
	})

	T.Run("rejects a negative retention", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Retention = -time.Second

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an invalid cache config", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Cache.Provider = "nonsense"

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an invalid lock config", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Lock.Provider = "nonsense"

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an unknown provider", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Provider = "nonsense"

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	// `env:",init"` leaves every sub-config populated, so the unselected
	// provider's rules would otherwise make a perfectly good configuration
	// unloadable. The lock goes with the cache, because it is the cache
	// provider that needs one.
	T.Run("skips the cache and the lock under the database provider", func(t *testing.T) {
		t.Parallel()

		cfg := databaseConfig()
		cfg.Cache.Provider = "nonsense"
		cfg.Lock.Provider = "nonsense"

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("skips the database under the cache provider", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Database.TablePrefix = "trailing_"

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an invalid database config under the database provider", func(t *testing.T) {
		t.Parallel()

		cfg := databaseConfig()
		cfg.Database.TablePrefix = "trailing_"

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewMinter(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		minter, err := NewMinter(t.Context(), memoryConfig(), nil)
		must.NoError(t, err)

		link, err := minter.Mint(t.Context(), "magic_login", "user_123")
		must.NoError(t, err)
		test.StrHasPrefix(t, "https://app.example.com/auth/magic/", link.URL)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewMinter(t.Context(), nil, nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
	})

	T.Run("reports a config with no actions", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Actions = nil

		_, err := NewMinter(t.Context(), cfg, nil)
		test.ErrorIs(t, err, links.ErrNoActions)
	})

	T.Run("reports an action policy the file got wrong", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Actions["verify_email"] = links.ActionPolicy{URL: "https://app.example.com/verify/{token}"}

		_, err := NewMinter(t.Context(), cfg, nil)
		test.ErrorIs(t, err, links.ErrInvalidTTL)
	})

	T.Run("refuses a cleartext action URL unless told otherwise", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Actions["magic_login"] = links.ActionPolicy{
			URL: "http://staging.example.com/auth/magic/{token}",
			TTL: time.Minute,
		}

		_, err := NewMinter(t.Context(), cfg, nil)
		test.ErrorIs(t, err, links.ErrInsecureActionURL)

		cfg.AllowInsecureURLs = true

		_, err = NewMinter(t.Context(), cfg, nil)
		test.NoError(t, err)
	})

	T.Run("caller options win over the file", func(t *testing.T) {
		t.Parallel()

		minter, err := NewMinter(t.Context(), memoryConfig(), nil,
			WithMinterOptions(links.WithAction("magic_login", links.ActionPolicy{
				URL: "https://tenant.example.com/auth/magic/{token}",
				TTL: time.Minute,
			})))
		must.NoError(t, err)

		link, err := minter.Mint(t.Context(), "magic_login", "user_123")
		must.NoError(t, err)
		test.StrHasPrefix(t, "https://tenant.example.com/auth/magic/", link.URL)
	})

	T.Run("caller options can register an action the file does not", func(t *testing.T) {
		t.Parallel()

		minter, err := NewMinter(t.Context(), memoryConfig(), nil,
			WithMinterOptions(links.WithAction("unsubscribe", links.ActionPolicy{
				URL: "https://app.example.com/unsubscribe?t={token}",
				TTL: 24 * time.Hour,
			})))
		must.NoError(t, err)

		test.SliceLen(t, 2, minter.Actions())
	})

	T.Run("nil options are ignored", func(t *testing.T) {
		t.Parallel()

		_, err := NewMinter(t.Context(), memoryConfig(), nil, nil)
		test.NoError(t, err)
	})

	T.Run("builds a durable store with no locker configured", func(t *testing.T) {
		t.Parallel()

		// The whole point of the provider: a deployment with a database and no
		// Redis mints and redeems links, and never names a lock service.
		cfg := databaseConfig()
		cfg.Lock = distributedlockcfg.Config{}

		db := testDBClient(t)
		must.NoError(t, createLinksTable(t, db))

		minter, err := NewMinter(t.Context(), cfg, db)
		must.NoError(t, err)

		link, err := minter.Mint(t.Context(), "magic_login", "user_123")
		must.NoError(t, err)

		claims, err := minter.Redeem(t.Context(), link.Token)
		must.NoError(t, err)
		test.EqOp(t, links.Subject("user_123"), claims.Subject)

		_, err = minter.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, links.ErrLinkAlreadyRedeemed)
	})

	T.Run("reports a database provider with no client", func(t *testing.T) {
		t.Parallel()

		_, err := NewMinter(t.Context(), databaseConfig(), nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
	})

	T.Run("rejects an unknown provider", func(t *testing.T) {
		t.Parallel()

		cfg := memoryConfig()
		cfg.Provider = "nonsense"

		_, err := NewMinter(t.Context(), cfg, nil)
		test.Error(t, err)
	})
}
