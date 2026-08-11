package secretscfg

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/internal/cfgnorm"
	"github.com/primandproper/platform-go/v10/secrets"
	"github.com/primandproper/platform-go/v10/secrets/env"
	"github.com/primandproper/platform-go/v10/secrets/gcp"
	"github.com/primandproper/platform-go/v10/secrets/kubernetes"
	"github.com/primandproper/platform-go/v10/secrets/noop"
	"github.com/primandproper/platform-go/v10/secrets/ssm"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderEnv represents environment variables.
	ProviderEnv = "env"
	// ProviderNoop represents the noop provider.
	ProviderNoop = "noop"
	// ProviderGCP represents GCP Secret Manager.
	ProviderGCP = "gcp"
	// ProviderSSM represents AWS SSM Parameter Store.
	ProviderSSM = "ssm"
	// ProviderKubernetes represents Kubernetes secrets.
	ProviderKubernetes = "kubernetes"
)

// Config configures secret source selection.
type Config struct {
	GCPClient        gcp.SecretVersionAccessor `json:"-"       yaml:"-"`
	SSMClient        ssm.GetParameterAPI       `json:"-"       yaml:"-"`
	KubernetesClient kubernetes.SecretGetter   `json:"-"       yaml:"-"`
	Env              *env.Config               `env:",init"    envPrefix:"ENV_"          json:"env,omitempty"        yaml:"env,omitempty"`
	GCP              *gcp.Config               `env:",init"    envPrefix:"GCP_"          json:"gcp,omitempty"        yaml:"gcp,omitempty"`
	SSM              *ssm.Config               `env:",init"    envPrefix:"SSM_"          json:"ssm,omitempty"        yaml:"ssm,omitempty"`
	Kubernetes       *kubernetes.Config        `env:",init"    envPrefix:"KUBERNETES_"   json:"kubernetes,omitempty" yaml:"kubernetes,omitempty"`
	Provider         string                    `env:"PROVIDER" json:"provider,omitempty" yaml:"provider,omitempty"`

	// CacheTTL, when positive, wraps the selected provider in a caching source
	// whose entries live this long. Leaving it unset returns the provider
	// undecorated, so every GetSecret is a round-trip — fine for the env
	// provider, expensive for the ones that talk to a network.
	CacheTTL time.Duration `env:"CACHE_TTL" json:"cacheTTL,omitempty" yaml:"cacheTTL,omitempty"`

	// RefreshInterval, when positive, keeps the cached entries warm in the
	// background instead of leaving each expiry to be paid for by whichever
	// caller arrives first. It requires CacheTTL and must be shorter than it;
	// see secrets.WithRefresh for why.
	RefreshInterval time.Duration `env:"REFRESH_INTERVAL" json:"refreshInterval,omitempty" yaml:"refreshInterval,omitempty"`
}

// providers are every provider this package implements, plus the empty string,
// which selects the env source. Validation and NewSecretSource both read it.
var providers = []string{"", ProviderEnv, ProviderNoop, ProviderGCP, ProviderSSM, ProviderKubernetes}

// normalizeProvider canonicalizes a provider name the way NewSecretSource does.
func normalizeProvider(provider string) string {
	return strings.TrimSpace(strings.ToLower(provider))
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the config.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	// Release the sub-configs env parsing's ",init" allocated and nothing filled
	// in, so the Nil rules below read "the operator configured this" rather than
	// "env parsing ran".
	cfgnorm.ZeroToNil(&cfg.GCP)
	cfgnorm.ZeroToNil(&cfg.SSM)
	cfgnorm.ZeroToNil(&cfg.Kubernetes)

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Provider, validation.By(func(any) error {
			// Checked normalized, matching dispatch: validating the raw string
			// rejected "GCP" and " gcp " while NewSecretSource accepted both.
			if !slices.Contains(providers, normalizeProvider(cfg.Provider)) {
				return errors.Wrapf(errors.ErrUnknownProvider, "secrets provider %q", cfg.Provider)
			}

			return nil
		})),
		validation.Field(&cfg.GCP, validation.When(normalizeProvider(cfg.Provider) == ProviderGCP, validation.Required), validation.When(normalizeProvider(cfg.Provider) != ProviderGCP, validation.Nil)),
		validation.Field(&cfg.SSM, validation.When(normalizeProvider(cfg.Provider) == ProviderSSM, validation.Required), validation.When(normalizeProvider(cfg.Provider) != ProviderSSM, validation.Nil)),
		validation.Field(&cfg.Kubernetes, validation.When(normalizeProvider(cfg.Provider) == ProviderKubernetes, validation.Required), validation.When(normalizeProvider(cfg.Provider) != ProviderKubernetes, validation.Nil)),
		validation.Field(&cfg.CacheTTL, validation.Min(time.Duration(0))),
		// Caught here as well as at construction, so an operator who set a
		// refresh without a cache — or one that cannot land before the entry it
		// refreshes expires — learns it from config validation instead of from
		// a source that quietly never refreshes.
		//
		// The messages are plain rather than wrapped around
		// secrets.ErrInvalidRefreshInterval because ozzo's validation.Errors is
		// a map with no Unwrap: a sentinel put in here would read as though
		// errors.Is could find it, and it could not.
		validation.Field(&cfg.RefreshInterval, validation.Min(time.Duration(0)), validation.By(func(any) error {
			if cfg.RefreshInterval <= 0 {
				return nil
			}

			if cfg.CacheTTL <= 0 {
				return errors.New("refresh interval requires a cache TTL")
			}

			if cfg.RefreshInterval >= cfg.CacheTTL {
				return errors.Newf("refresh interval %s must be shorter than the cache TTL %s", cfg.RefreshInterval, cfg.CacheTTL)
			}

			return nil
		})),
	)
}

// NewSecretSource returns a SecretSource from config.
//
// When CacheTTL is set, what comes back is the selected provider wrapped in
// secrets.NewCachingSource — a decorator, so the returned value is still just a
// SecretSource and no call site changes. A caller that wants the rotation hooks
// asserts for secrets.CachingSource, which succeeds exactly when caching is
// configured:
//
//	if cached, ok := source.(secrets.CachingSource); ok {
//		cancel := cached.OnChange("signing-key", rebuildKeyring)
//	}
func (cfg *Config) NewSecretSource(ctx context.Context, opts ...Option) (secrets.SecretSource, error) {
	o := newOptions(opts)

	if cfg == nil {
		s, err := env.NewSecretSource(env.WithLogger(o.logger), env.WithTracerProvider(o.tracerProvider), env.WithMetricsProvider(o.metricsProvider))
		if err != nil {
			return nil, err
		}

		return s, nil
	}

	source, err := cfg.newProviderSource(ctx, o)
	if err != nil {
		return nil, err
	}

	return cfg.decorate(ctx, source, o)
}

// newProviderSource builds the undecorated source the Provider names.
//
// Each provider is built into a variable and returned only once its error is
// known to be nil. The provider constructors return their own concrete types,
// so returning one straight from the constructor would convert a nil
// *SecretSource into a non-nil secrets.SecretSource on the error path, and a
// caller testing the returned interface against nil would find a source that
// panics on first use. noop.NewSecretSource cannot fail and needs no such care.
func (cfg *Config) newProviderSource(ctx context.Context, o *options) (secrets.SecretSource, error) {
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	provider := normalizeProvider(cfg.Provider)
	switch provider {
	case "", ProviderEnv:
		s, err := env.NewSecretSource(env.WithLogger(logger), env.WithTracerProvider(tracerProvider), env.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return s, nil
	case ProviderNoop:
		return noop.NewSecretSource(), nil
	case ProviderGCP:
		if cfg.GCP == nil {
			return nil, errors.New("gcp provider requires gcp config")
		}

		s, err := gcp.NewSecretSource(ctx, cfg.GCP, cfg.GCPClient, gcp.WithLogger(logger), gcp.WithTracerProvider(tracerProvider), gcp.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return s, nil
	case ProviderSSM:
		if cfg.SSM == nil {
			return nil, errors.New("ssm provider requires ssm config")
		}

		s, err := ssm.NewSecretSource(ctx, cfg.SSM, cfg.SSMClient, ssm.WithLogger(logger), ssm.WithTracerProvider(tracerProvider), ssm.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return s, nil
	case ProviderKubernetes:
		if cfg.Kubernetes == nil {
			return nil, errors.New("kubernetes provider requires kubernetes config")
		}

		s, err := kubernetes.NewSecretSource(ctx, cfg.Kubernetes, cfg.KubernetesClient, kubernetes.WithLogger(logger), kubernetes.WithTracerProvider(tracerProvider), kubernetes.WithMetricsProvider(metricsProvider))
		if err != nil {
			return nil, err
		}

		return s, nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "secrets provider %q", cfg.Provider)
	}
}

// decorate wraps source in a caching source when a TTL is configured, and
// returns it untouched otherwise.
//
// The refresh, when configured, runs against ctx — the same context that built
// the source — so a caller whose context ends stops refreshing even if it never
// gets around to closing what it was given.
func (cfg *Config) decorate(ctx context.Context, source secrets.SecretSource, o *options) (secrets.SecretSource, error) {
	if cfg.CacheTTL <= 0 {
		return source, nil
	}

	cachingOpts := []secrets.Option{
		secrets.WithLogger(o.logger),
		secrets.WithTracerProvider(o.tracerProvider),
		secrets.WithMetricsProvider(o.metricsProvider),
	}

	if cfg.RefreshInterval > 0 {
		cachingOpts = append(cachingOpts, secrets.WithRefresh(ctx, cfg.RefreshInterval))
	}

	// Appended last so a caller's explicit option beats what the config
	// derived, matching the order-of-application rule the options themselves
	// document.
	cachingOpts = append(cachingOpts, o.cachingOptions...)

	// The context reaches the caching source through WithRefresh above rather
	// than as a parameter, because construction itself does no I/O — the only
	// thing there is a context for is the lifetime of the refresh goroutine, and
	// a source built without one has nothing to cancel.
	cached, err := secrets.NewCachingSource(source, cfg.CacheTTL, cachingOpts...) //nolint:contextcheck // ctx is carried by WithRefresh; see above.
	if err != nil {
		// The provider source was built and is about to be dropped. Nothing
		// else holds it, so this is the only chance to release whatever client
		// it opened.
		if closeErr := source.Close(); closeErr != nil {
			err = errors.Join(err, errors.Wrap(closeErr, "closing the undecorated secret source"))
		}

		return nil, errors.Wrap(err, "wrapping the secret source in a cache")
	}

	return cached, nil
}
