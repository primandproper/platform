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
	"slices"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v10/authorization"
	"github.com/primandproper/platform-go/v10/authorization/cached"
	authzdb "github.com/primandproper/platform-go/v10/authorization/database"
	"github.com/primandproper/platform-go/v10/authorization/static"
	"github.com/primandproper/platform-go/v10/cache"
	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/internal/cfgnorm"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"

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
	Database *authzdb.Config `env:",init" envPrefix:"DATABASE_" json:"database,omitempty" yaml:"database,omitempty"`
	// Provider selects the implementation. Empty means ProviderStatic.
	Provider string `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`
	// Roles is the policy for the static provider. It is loadable from JSON or
	// YAML, so a static deployment can change policy by shipping config rather
	// than code.
	Roles []authorization.Role `json:"roles,omitempty" yaml:"roles,omitempty"`
	// CacheTTL sets how long a resolution is cached when a cache is supplied to
	// NewPolicyResolver. Zero uses the cached package's default.
	CacheTTL time.Duration `env:"CACHE_TTL" json:"cacheTTL,omitempty" yaml:"cacheTTL,omitempty"`
}

// providers are every provider this package implements, plus the empty string,
// which selects the static resolver. Validation and dispatch both read it.
var providers = []string{"", ProviderStatic, ProviderDatabase}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	provider := normalize(cfg.Provider)

	// Release the sub-configs env parsing's ",init" allocated and nothing filled
	// in, so the Nil rules below read "the operator configured this" rather than
	// "env parsing ran".
	cfgnorm.ZeroToNil(&cfg.Database)

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.By(func(any) error {
			// Checked normalized, matching dispatch: validating the raw string
			// rejected "Static" and " static " while the factory accepted both.
			if !slices.Contains(providers, provider) {
				return errors.Wrapf(errors.ErrUnknownProvider, "authorization provider %q", cfg.Provider)
			}

			return nil
		})),
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

// options collects the passthrough options for each backend, plus the
// observability the resolver is built with.
type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider

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

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider, enabling spans on policy
// resolution. An absent tracer provider traces nowhere.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *options) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider. An absent provider records
// nothing.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *options) { o.metricsProvider = metricsProvider }
}

// WithPillars attaches a logger, tracer provider, and metrics provider in one
// go, for the common case where a caller has already built them together. A nil
// Pillars attaches nothing.
//
// It is applied in order with the individual options, so a caller can hand over
// its pillars and then override one of them.
func WithPillars(p *observability.Pillars) Option {
	return func(o *options) { o.logger, o.tracerProvider, o.metricsProvider = p.Deps() }
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
	db database.SQLQueryExecutor,
	c cache.Cache[authorization.PermissionSet],
	opts ...Option,
) (authorization.PolicyResolver, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

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
