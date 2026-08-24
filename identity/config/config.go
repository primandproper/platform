/*
Package identitycfg assembles an identity Store from environment configuration.

There is one thing to configure and one thing to build, which is why this
package is smaller than most of its siblings: the dialect comes from the
database.Client so it cannot disagree with the database the statements run
against, and everything else about the store is either the schema's or an
option.

The table prefix is the exception, and it has to be here because it must match
the prefix the migrations were rendered with — a deployment sharing one database
between applications sets both from the same value.
*/
package identitycfg

import (
	"context"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/identity"
	"github.com/primandproper/platform-go/v13/identity/migrations"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config assembles an identity Store.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// TablePrefix names the six identity tables. It must match the prefix the
	// migrations were rendered with. Defaults to identity.DefaultTablePrefix.
	TablePrefix string `env:"TABLE_PREFIX" json:"tablePrefix,omitempty" yaml:"tablePrefix,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
func (cfg *Config) EnsureDefaults() {
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = identity.DefaultTablePrefix
	}
}

// ValidateWithContext validates a Config.
//
// The prefix is vetted against the identifiers it actually renders rather than
// against a pattern, so a prefix that is legal in isolation but produces an
// over-long index name fails here instead of at the first migration.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	if err := validation.ValidateStructWithContext(ctx, cfg); err != nil {
		return err
	}

	return migrations.ValidatePrefix(cfg.TablePrefix)
}

// NewStore builds the Store. client must be the database holding the identity
// tables.
//
// Each provider is built into a variable and returned only once its error is
// known to be nil. The provider constructors return their own concrete types,
// so returning one straight through would convert a nil *identity.SQLStore into
// a non-nil identity.Store on the error path, and a caller testing the result
// against nil would find a store that panics on first use.
func NewStore(ctx context.Context, cfg *Config, client database.Client, opts ...Option) (identity.Store, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating identity config")
	}

	options := newOptions(opts)

	base := []identity.SQLStoreOption{
		identity.WithTablePrefix(cfg.TablePrefix),
		identity.WithStoreLogger(options.logger),
		identity.WithStoreTracerProvider(options.tracerProvider),
		identity.WithStoreMetricsProvider(options.metricsProvider),
	}

	store, storeErr := identity.NewSQLStore(client, append(base, options.store...)...)
	if storeErr != nil {
		return nil, storeErr
	}

	return store, nil
}
