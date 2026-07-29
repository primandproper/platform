// Package authorizationcfg selects and builds an
// authorization.PolicyResolver from configuration.
//
// The zero value works: an empty Provider selects the static resolver, which
// needs no database, no migrations, and no configuration. Set Provider to
// "database" to opt into SQL-backed policy — deliberately opt-in, so that a
// newcomer does not inherit the operational cost of the heavier backend just
// because some consumer runs it.
//
// Supplying a cache wraps whichever resolver is chosen in authorization/cached,
// which is what turns the database provider from a query per session build into
// a query per policy change.
package authorizationcfg

import (
	"context"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v8/authorization"
	"github.com/primandproper/platform-go/v8/authorization/cached"
	authzdb "github.com/primandproper/platform-go/v8/authorization/database"
	"github.com/primandproper/platform-go/v8/authorization/static"
	"github.com/primandproper/platform-go/v8/cache"
	"github.com/primandproper/platform-go/v8/database"
	"github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderStatic resolves policy declared at build time or loaded from
	// config. It is the default, and an empty Provider selects it.
	ProviderStatic = "static"
	// ProviderDatabase resolves policy from SQL tables, for deployments where
	// roles must be editable without a release.
	ProviderDatabase = "database"
)

// Config configures a policy resolver.
//
// The zero value is valid and yields a working static resolver that grants
// nothing. That is deliberate on both counts: the most accessible
// implementation is the default so the package runs with no infrastructure,
// and an unconfigured authorization layer denies rather than admits.
type Config struct {
	// Database configures the database provider. Required when Provider is
	// "database", and must be absent otherwise.
	Database *authzdb.Config `env:"init" envPrefix:"DATABASE_" json:"database,omitempty" yaml:"database,omitempty"`
	// Provider selects the implementation. Empty means ProviderStatic.
	Provider string `env:"PROVIDER" json:"provider" yaml:"provider"`
	// Roles is the policy for the static provider. It is loadable from JSON or
	// YAML, so a static deployment can change policy by shipping config rather
	// than code.
	Roles []authorization.Role `json:"roles,omitempty" yaml:"roles,omitempty"`
	// CacheTTL sets how long a resolution is cached when a cache is supplied to
	// NewPolicyResolver. Zero uses the cached package's default.
	CacheTTL time.Duration `env:"CACHE_TTL" json:"cacheTTL" yaml:"cacheTTL"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	provider := normalize(cfg.Provider)

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.In(ProviderStatic, ProviderDatabase, "")),
		validation.Field(&cfg.Database,
			validation.When(provider == ProviderDatabase, validation.Required),
			validation.When(provider != ProviderDatabase, validation.Nil),
		),
	)
}

// NewPolicyResolver builds the configured resolver.
//
// db is used only by the database provider and may be nil otherwise. c is
// optional: when non-nil the resolver is wrapped in authorization/cached, which
// is what turns the database provider from a query per session build into a
// query per policy change.
func NewPolicyResolver(
	_ context.Context,
	cfg *Config,
	db database.SQLQueryExecutor,
	c cache.Cache[authorization.PermissionSet],
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
) (authorization.PolicyResolver, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	var (
		resolver authorization.PolicyResolver
		err      error
	)

	switch normalize(cfg.Provider) {
	case ProviderDatabase:
		if cfg.Database == nil {
			return nil, errors.New("database authorization provider selected with no database config")
		}
		resolver, err = authzdb.NewResolver(cfg.Database, db,
			authzdb.WithLogger(logger),
			authzdb.WithTracerProvider(tracerProvider),
			authzdb.WithMetricsProvider(metricsProvider),
		)
	case ProviderStatic, "":
		resolver, err = static.NewResolver(cfg.Roles, static.WithLogger(logger))
	default:
		return nil, errors.Newf("invalid authorization provider: %q", cfg.Provider)
	}

	if err != nil {
		return nil, err
	}

	if c == nil {
		return resolver, nil
	}

	return cached.NewResolver(resolver, c,
		cached.WithLogger(logger),
		cached.WithTracerProvider(tracerProvider),
		cached.WithMetricsProvider(metricsProvider),
		cached.WithTTL(cfg.CacheTTL),
	)
}

// normalize trims and lowercases a provider name, so that configuration
// supplied by hand is not defeated by whitespace or case.
func normalize(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}
