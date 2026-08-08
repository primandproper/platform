package secretscfg

import (
	"context"
	"slices"
	"strings"

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
	)
}

// NewSecretSource returns a SecretSource from config.
func (cfg *Config) NewSecretSource(ctx context.Context, opts ...Option) (secrets.SecretSource, error) {
	o := newOptions(opts)
	logger, tracerProvider, metricsProvider := o.logger, o.tracerProvider, o.metricsProvider

	if cfg == nil {
		return env.NewSecretSource(env.WithLogger(logger), env.WithTracerProvider(tracerProvider), env.WithMetricsProvider(metricsProvider))
	}

	provider := normalizeProvider(cfg.Provider)
	switch provider {
	case "", ProviderEnv:
		return env.NewSecretSource(env.WithLogger(logger), env.WithTracerProvider(tracerProvider), env.WithMetricsProvider(metricsProvider))
	case ProviderNoop:
		return noop.NewSecretSource(), nil
	case ProviderGCP:
		if cfg.GCP == nil {
			return nil, errors.New("gcp provider requires gcp config")
		}
		return gcp.NewSecretSource(ctx, cfg.GCP, cfg.GCPClient, gcp.WithLogger(logger), gcp.WithTracerProvider(tracerProvider), gcp.WithMetricsProvider(metricsProvider))
	case ProviderSSM:
		if cfg.SSM == nil {
			return nil, errors.New("ssm provider requires ssm config")
		}
		return ssm.NewSecretSource(ctx, cfg.SSM, cfg.SSMClient, ssm.WithLogger(logger), ssm.WithTracerProvider(tracerProvider), ssm.WithMetricsProvider(metricsProvider))
	case ProviderKubernetes:
		if cfg.Kubernetes == nil {
			return nil, errors.New("kubernetes provider requires kubernetes config")
		}
		return kubernetes.NewSecretSource(ctx, cfg.Kubernetes, cfg.KubernetesClient, kubernetes.WithLogger(logger), kubernetes.WithTracerProvider(tracerProvider), kubernetes.WithMetricsProvider(metricsProvider))
	default:
		return nil, errors.Newf("unknown secret source provider: %q", cfg.Provider)
	}
}
