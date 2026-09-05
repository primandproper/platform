/*
Package linkscfg assembles a links.Minter from environment configuration.
Records live either in a cache — the default — or in a SQL table.

The choice is not a performance one. A link is minted by whatever builds the
email and redeemed by whatever serves the click, so both providers exist to be
reachable from more than one process; what separates them is what a deployment
already runs. The cache provider wants Redis and a lock service, because a cache
cannot make the read and the write of a redemption one operation. The database
provider wants neither: a guarded UPDATE inside a transaction is the same
promise, so an application with Postgres and no Redis is not excluded from three
of its four link flows. It is also the only provider with rows to sweep, which
is what SweepInterval is for.

The action registry is the one part that does not come from the environment.
Where a magic-login link points and how long it lives is a security policy, not
a deployment knob, and it belongs in a file somebody reviews — see Config.Actions.
*/
package linkscfg

import (
	"context"
	"strings"
	"time"

	cachecfg "github.com/primandproper/platform-go/v14/cache/config"
	"github.com/primandproper/platform-go/v14/database"
	distributedlockcfg "github.com/primandproper/platform-go/v14/distributedlock/config"
	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/internal/cfgnorm"
	"github.com/primandproper/platform-go/v14/links"
	linkscache "github.com/primandproper/platform-go/v14/links/cache"
	linksdatabase "github.com/primandproper/platform-go/v14/links/database"
	"github.com/primandproper/platform-go/v14/pointer"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderCache stores link records in a cache — redis for a fleet, memory
	// for tests. The default answer, and the one that also needs a locker.
	ProviderCache = "cache"
	// ProviderDatabase stores link records in a SQL table, for a deployment
	// with a database and no Redis. It needs no locker: single use is the
	// affected row count of a guarded UPDATE inside a transaction.
	ProviderDatabase = "database"
)

// DefaultSweepInterval is how often the database provider removes rows past
// their purge deadline, when nothing says otherwise. It is ignored by the cache
// provider, which reclaims its own entries.
const DefaultSweepInterval = 5 * time.Minute

// Config assembles a links.Minter from environment configuration.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// Actions declares the links this deployment can mint, keyed by action.
	//
	// It carries no env tag, and that is deliberate rather than a limitation of
	// the encoding. A URL and a lifetime per action have no reasonable flat
	// environment spelling, and more to the point this is the file where
	// "password reset links live for one hour and point at this host" is
	// written down — a decision that should appear in a diff and be reviewed,
	// not one that should be adjustable by whoever can edit a deployment
	// variable.
	//
	// A caller assembling actions in code should use WithMinterOptions and
	// links.WithAction instead; the two compose, and the explicit options win.
	Actions map[links.Action]links.ActionPolicy `json:"actions,omitempty" yaml:"actions,omitempty"`

	// SweepInterval is how often the database provider removes rows past their
	// purge deadline. It is ignored by the cache provider. Unset takes
	// DefaultSweepInterval; zero starts no sweeper.
	//
	// Starting no sweeper is right when a scheduler calls Sweep instead — one
	// sweep for the fleet rather than one per replica — and wrong when nothing
	// else does, since the table then grows by a row for every link ever
	// minted. That asymmetry is why the sweeper is what an unconfigured
	// database Config gets, and why the pointer is here: unset and zero are
	// different answers, and a time.Duration has only one way to say both.
	//
	// In the environment that is an absent SWEEP_INTERVAL against
	// SWEEP_INTERVAL=0.
	SweepInterval *time.Duration `env:"SWEEP_INTERVAL" json:"sweepInterval,omitempty" yaml:"sweepInterval,omitempty"`

	// Provider selects where link records live: cache or database.
	Provider string `env:"PROVIDER" envDefault:"cache" json:"provider,omitempty" yaml:"provider,omitempty"`

	// KeyPrefix namespaces the cache provider's record and lock keys. It is
	// ignored by the database provider, which namespaces a table instead — see
	// Database.
	KeyPrefix string `env:"KEY_PREFIX" json:"keyPrefix,omitempty" yaml:"keyPrefix,omitempty"`

	// Lock configures the locker that makes redemption single-use under the
	// cache provider. It has no safe default there: the noop provider acquires
	// unconditionally, which leaves every sequential test passing and lets two
	// concurrent redemptions of one token both succeed.
	//
	// It is unread under the database provider, which needs no lock service.
	Lock distributedlockcfg.Config `env:",init" envPrefix:"LOCK_" json:"lock,omitzero" yaml:"lock,omitempty"`

	// Database configures the store when Provider is database. The dialect
	// comes from the database.Client rather than from here.
	Database linksdatabase.Config `env:",init" envPrefix:"DATABASE_" json:"database,omitzero" yaml:"database,omitempty"`

	// Cache configures the store when Provider is cache. Use the redis provider
	// in production: the memory provider is per-process, so a link minted by one
	// replica does not exist for the next.
	Cache cachecfg.Config `env:",init" envPrefix:"CACHE_" json:"cache,omitzero" yaml:"cache,omitempty"`

	// Retention is how long a resolved link stays in the store after it stops
	// working, and so how long redemption can still say why it failed.
	Retention time.Duration `env:"RETENTION" json:"retention,omitempty" yaml:"retention,omitempty"`

	// TokenBytes is how many random bytes a token carries before encoding.
	TokenBytes int `env:"TOKEN_BYTES" json:"tokenBytes,omitempty" yaml:"tokenBytes,omitempty"`

	// MaxTokenLength bounds what a redemption will hash.
	MaxTokenLength int `env:"MAX_TOKEN_LENGTH" json:"maxTokenLength,omitempty" yaml:"maxTokenLength,omitempty"`

	// AllowInsecureURLs permits http action URLs against hosts that are not
	// loopback, which hands the token to every hop between the mail client and
	// the application. Loopback http already works without it — see
	// links.WithInsecureURLs.
	AllowInsecureURLs bool `env:"ALLOW_INSECURE_URLS" json:"allowInsecureURLs,omitempty" yaml:"allowInsecureURLs,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
//
// Nothing here defaults an action's TTL. links.ActionPolicy has no default
// lifetime on purpose, and inventing one at the configuration layer would put
// it back exactly where it was rejected.
//
// SweepInterval is the one field where defaulting requires a pointer: only a
// nil is unset, so a zero reaching this method is a deployment asking for no
// sweeper and is left alone. It is deliberately not defaulted for the cache
// provider, which has nothing to sweep.
func (cfg *Config) EnsureDefaults() {
	if cfg.Provider == "" {
		cfg.Provider = ProviderCache
	}
	if cfg.Retention == 0 {
		cfg.Retention = links.DefaultRetention
	}
	if cfg.TokenBytes == 0 {
		cfg.TokenBytes = links.DefaultTokenBytes
	}
	if cfg.MaxTokenLength == 0 {
		cfg.MaxTokenLength = links.DefaultMaxTokenLength
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = linkscache.DefaultKeyPrefix
	}
	if cfg.provider() == ProviderDatabase {
		cfg.SweepInterval = cfgnorm.EnsureSweepInterval(cfg.SweepInterval, DefaultSweepInterval)
	}
}

// ValidateWithContext validates a Config struct.
//
// The action policies are not validated here. NewMinter validates them against
// the insecure-URL setting, which is where the whole registry is visible at
// once — including the actions a caller added through WithMinterOptions, which
// this Config has never seen.
//
// The sub-config for a provider that was not selected is skipped rather than
// merely unguarded: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config populated.
// Validating the cache's rules under the database provider would make a
// perfectly good database configuration unloadable. The lock goes with the
// cache, because it is the cache provider that needs one.
//
// The nested configs are validated through validation.By closures because ozzo
// dereferences a struct-value field before checking ValidatableWithContext, so
// it would otherwise skip them.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.In(ProviderCache, ProviderDatabase)),
		validation.Field(&cfg.Retention, validation.Min(time.Duration(0))),
		validation.Field(&cfg.TokenBytes, validation.Min(0)),
		validation.Field(&cfg.MaxTokenLength, validation.Min(0)),
		validation.Field(&cfg.Cache,
			validation.Skip.When(cfg.provider() != ProviderCache),
			validation.By(func(any) error { return cfg.Cache.ValidateWithContext(ctx) })),
		validation.Field(&cfg.Lock,
			validation.Skip.When(cfg.provider() != ProviderCache),
			validation.By(func(any) error { return cfg.Lock.ValidateWithContext(ctx) })),
		validation.Field(&cfg.Database,
			validation.Skip.When(cfg.provider() != ProviderDatabase),
			validation.By(func(any) error { return cfg.Database.ValidateWithContext(ctx) })),
		validation.Field(&cfg.SweepInterval, cfgnorm.SweepIntervalRule),
	)
}

// provider normalizes the configured provider name, so that trailing whitespace
// out of an environment file is not a different provider.
func (cfg *Config) provider() string {
	return strings.TrimSpace(strings.ToLower(cfg.Provider))
}

// NewMinter builds a links.Minter from configuration.
//
// db is required when the provider is database, and when the cache provider's
// lock provider is postgres; pass nil otherwise.
func NewMinter(
	ctx context.Context,
	cfg *Config,
	db database.Client,
	opts ...Option,
) (*links.Minter, error) {
	o := newOptions(opts)

	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating links config")
	}

	store, err := newStore(ctx, cfg, db, o)
	if err != nil {
		return nil, err
	}

	minterOpts := []links.Option{
		links.WithActions(cfg.Actions),
		links.WithRetention(cfg.Retention),
		links.WithTokenBytes(cfg.TokenBytes),
		links.WithMaxTokenLength(cfg.MaxTokenLength),
		links.WithLogger(o.logger),
		links.WithTracerProvider(o.tracerProvider),
		links.WithMetricsProvider(o.metricsProvider),
	}

	// Conditional rather than always applied: links.WithInsecureURLs takes no
	// argument, so there is no value of it that means "keep requiring https".
	if cfg.AllowInsecureURLs {
		minterOpts = append(minterOpts, links.WithInsecureURLs())
	}

	// Caller options are appended last so they win over anything configured.
	return links.NewMinter(store, append(minterOpts, o.minter...)...)
}

// newStore selects and builds the storage the configured provider names.
//
// An unrecognized provider is an error rather than a working-looking default.
// Falling back to the memory cache would produce a service that mints links in
// one process and cannot redeem them in another, and looks configured the whole
// time.
func newStore(
	ctx context.Context,
	cfg *Config,
	db database.Client,
	o *options,
) (links.Store, error) {
	// Every branch builds into store and returns it only once err is known to
	// be nil. Both constructors hand back a concrete pointer, and returning one
	// straight through would convert a nil *linkscache.Store into a non-nil
	// links.Store on the error path — a value that passes a caller's nil check
	// and panics on the first Get.
	var (
		store links.Store
		err   error
	)

	switch cfg.provider() {
	case ProviderCache:
		// cacheErr and lockErr rather than the err above: these are returned on
		// the spot, and := here would shadow err for the rest of the case,
		// leaving the store assignment below writing to a variable nothing
		// reads.
		c, cacheErr := cachecfg.NewCache[links.Record](ctx, &cfg.Cache,
			cachecfg.WithLogger(o.logger),
			cachecfg.WithTracerProvider(o.tracerProvider),
			cachecfg.WithMetricsProvider(o.metricsProvider))
		if cacheErr != nil {
			return nil, errors.Wrap(cacheErr, "building action link cache")
		}

		locker, lockErr := distributedlockcfg.NewScopedLocker(ctx, &cfg.Lock, db,
			distributedlockcfg.WithLogger(o.logger),
			distributedlockcfg.WithTracerProvider(o.tracerProvider),
			distributedlockcfg.WithMetricsProvider(o.metricsProvider))
		if lockErr != nil {
			return nil, errors.Wrap(lockErr, "building action link locker")
		}

		store, err = linkscache.New(c, locker, append([]linkscache.Option{
			linkscache.WithKeyPrefix(cfg.KeyPrefix),
			linkscache.WithLogger(o.logger),
			linkscache.WithTracerProvider(o.tracerProvider),
		}, o.cacheStore...)...)
	case ProviderDatabase:
		store, err = linksdatabase.New(&cfg.Database, db, append([]linksdatabase.Option{
			linksdatabase.WithLogger(o.logger),
			linksdatabase.WithTracerProvider(o.tracerProvider),
			linksdatabase.WithMetricsProvider(o.metricsProvider),
			// Bound to the caller's context: the sweep stops when whatever
			// scope owns this Minter does.
			linksdatabase.WithSweeper(ctx, pointer.Dereference(cfg.SweepInterval)),
		}, o.databaseStore...)...)
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "action link provider %q", cfg.Provider)
	}

	if err != nil {
		return nil, err
	}

	return store, nil
}
