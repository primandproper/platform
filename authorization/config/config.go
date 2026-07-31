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
// a query per policy change. Because that wrapping is decided here rather than
// by the caller, a process that edits policy reaches invalidation by asserting
// authorization.PolicyInvalidator on the returned resolver rather than by
// naming a concrete type.
package authorizationcfg

import (
	"context"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v9/authorization"
	"github.com/primandproper/platform-go/v9/authorization/cached"
	authzdb "github.com/primandproper/platform-go/v9/authorization/database"
	"github.com/primandproper/platform-go/v9/authorization/static"
	"github.com/primandproper/platform-go/v9/cache"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"

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

// Option configures how NewPolicyResolver assembles its resolver.
//
// The backend options are passthroughs, each applying only when configuration
// selects its backend, so one wiring site can carry options for whichever
// provider a given deployment turns out to run. They are appended after the
// options this package derives from its arguments, so a caller can override
// what it would otherwise be given.
type Option func(*options)

// options collects the passthrough options for each backend.
type options struct {
	static   []static.Option
	database []authzdb.Option
	cached   []cached.Option
}

// newOptions applies opts, ignoring nil entries.
func newOptions(opts []Option) *options {
	o := &options{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithStaticOptions passes opts to the static resolver, when the static
// provider is selected.
func WithStaticOptions(opts ...static.Option) Option {
	return func(o *options) { o.static = append(o.static, opts...) }
}

// WithDatabaseOptions passes opts to the database resolver, when the database
// provider is selected.
func WithDatabaseOptions(opts ...authzdb.Option) Option {
	return func(o *options) { o.database = append(o.database, opts...) }
}

// WithCachedOptions passes opts to the caching decorator, which is applied only
// when a cache is supplied.
func WithCachedOptions(opts ...cached.Option) Option {
	return func(o *options) { o.cached = append(o.cached, opts...) }
}

// NewPolicyResolver builds the configured resolver.
//
// db is used only by the database provider and may be nil otherwise. c is
// optional: when non-nil the resolver is wrapped in authorization/cached, which
// is what turns the database provider from a query per session build into a
// query per policy change.
//
// The result is an interface, so a caller that needs to drop cached policy
// after an edit type-asserts authorization.PolicyInvalidator rather than a
// concrete type — whether a cache is in the chain is this function's decision,
// not the caller's.
func NewPolicyResolver(
	_ context.Context,
	cfg *Config,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	db database.SQLQueryExecutor,
	c cache.Cache[authorization.PermissionSet],
	opts ...Option,
) (authorization.PolicyResolver, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	o := newOptions(opts)

	var (
		resolver authorization.PolicyResolver
		err      error
	)

	switch normalize(cfg.Provider) {
	case ProviderDatabase:
		if cfg.Database == nil {
			return nil, errors.New("database authorization provider selected with no database config")
		}
		resolver, err = authzdb.NewResolver(cfg.Database, db, append([]authzdb.Option{
			authzdb.WithLogger(logger),
			authzdb.WithTracerProvider(tracerProvider),
			authzdb.WithMetricsProvider(metricsProvider),
		}, o.database...)...)
	case ProviderStatic, "":
		resolver, err = static.NewResolver(cfg.Roles, append([]static.Option{
			static.WithLogger(logger),
		}, o.static...)...)
	default:
		return nil, errors.Newf("invalid authorization provider: %q", cfg.Provider)
	}

	if err != nil {
		return nil, err
	}

	if c == nil {
		return resolver, nil
	}

	return cached.NewResolver(resolver, c, append([]cached.Option{
		cached.WithLogger(logger),
		cached.WithTracerProvider(tracerProvider),
		cached.WithMetricsProvider(metricsProvider),
		cached.WithTTL(cfg.CacheTTL),
	}, o.cached...)...)
}

// normalize trims and lowercases a provider name, so that configuration
// supplied by hand is not defeated by whitespace or case.
func normalize(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}
