package authorizationcfg

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v14/authorization"
	"github.com/primandproper/platform-go/v14/authorization/cached"
	"github.com/primandproper/platform-go/v14/authorization/static"
	"github.com/primandproper/platform-go/v14/cache"
	"github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/observability/logging"
	"github.com/primandproper/platform-go/v14/observability/metrics"
	"github.com/primandproper/platform-go/v14/observability/tracing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config configures a policy resolver.
//
// The zero value is valid and yields a working static resolver that grants
// nothing.
type Config struct {
	// Roles is the policy for the static resolver. It is loadable from JSON or
	// YAML, so a static deployment can change policy by shipping config rather
	// than code.
	Roles []authorization.Role `json:"roles,omitempty" yaml:"roles,omitempty"`
	// CacheTTL sets how long a resolution is cached when a cache is supplied to
	// NewPolicyResolver. Zero uses the cached package's default.
	CacheTTL time.Duration `env:"CACHE_TTL" json:"cacheTTL,omitempty" yaml:"cacheTTL,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates a Config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		// cached.WithTTL folds anything that is not positive into "leave the
		// default", so without this rule a negative TTL is a deployment asking
		// for something it silently does not get. Zero still means the default,
		// which is what an unset field is.
		validation.Field(&cfg.CacheTTL, validation.Min(time.Duration(0))),
	)
}

// Option configures how NewPolicyResolver assembles its resolver.
//
// The backend options are passthroughs, each applying only when the backend it
// names is built, so one wiring site can carry options for whichever shape a
// given deployment turns out to run. They are appended after the options this
// package derives from its arguments, so a caller can override what it would
// otherwise be given.
type Option func(*options)

// options collects the passthrough options for each backend, plus the
// observability the resolver is built with.
type options struct {
	logger          logging.Logger
	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider

	static []static.Option
	cached []cached.Option
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

// WithStaticOptions passes opts to the static resolver.
func WithStaticOptions(opts ...static.Option) Option {
	return func(o *options) { o.static = append(o.static, opts...) }
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

// NewPolicyResolver builds the static resolver from cfg, wrapped in
// authorization/cached when c is non-nil.
//
// c is optional. When it is nil the resolver answers from its own declarations
// every time, which for the static resolver is a map lookup.
//
// The result is an interface, so a caller that needs to drop cached policy
// after an edit type-asserts authorization.PolicyInvalidator rather than a
// concrete type — whether a cache is in the chain is this function's decision,
// not the caller's.
//
// A deployment resolving policy from SQL calls authzdbcfg.NewPolicyResolver
// instead; it selects between that store and this resolver, and delegates here
// for the half it does not own.
func NewPolicyResolver(
	ctx context.Context,
	cfg *Config,
	c cache.Cache[authorization.PermissionSet],
	opts ...Option,
) (authorization.PolicyResolver, error) {
	// A nil config is the zero config, which this package documents as valid.
	// It is still put through validation below, so the two spellings of
	// "unconfigured" cannot diverge.
	if cfg == nil {
		cfg = &Config{}
	}

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating authorization config")
	}

	o := newOptions(opts)

	// Built into a variable and returned only once err is known to be nil:
	// static.NewResolver returns a *static.Resolver, and returning it straight
	// through would hand back a non-nil PolicyResolver wrapping a nil pointer
	// whenever it failed.
	resolver, err := static.NewResolver(cfg.Roles, append([]static.Option{
		static.WithLogger(o.logger),
	}, o.static...)...)
	if err != nil {
		return nil, err
	}

	return NewCachedResolver(cfg, resolver, c, opts...)
}

// NewCachedResolver wraps resolver in authorization/cached when c is non-nil,
// and hands it back untouched when c is nil.
//
// It is exported for authzdbcfg, which builds the SQL-backed resolver this
// package cannot and then needs the same wrapping applied to it. Keeping the
// CacheTTL read and the cached.NewResolver call in one place is the point: a
// second copy on the database branch could drift from this one, and nothing
// would say so.
func NewCachedResolver(
	cfg *Config,
	resolver authorization.PolicyResolver,
	c cache.Cache[authorization.PermissionSet],
	opts ...Option,
) (authorization.PolicyResolver, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	if c == nil {
		return resolver, nil
	}

	o := newOptions(opts)

	// Built into a variable and returned only once err is known to be nil, for
	// the reason NewPolicyResolver gives above.
	cachedResolver, err := cached.NewResolver(resolver, c, append([]cached.Option{
		cached.WithLogger(o.logger),
		cached.WithTracerProvider(o.tracerProvider),
		cached.WithMetricsProvider(o.metricsProvider),
		cached.WithTTL(cfg.CacheTTL),
	}, o.cached...)...)
	if err != nil {
		return nil, err
	}

	return cachedResolver, nil
}
