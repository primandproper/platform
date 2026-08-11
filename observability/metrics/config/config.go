package metricscfg

import (
	"context"
	"slices"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/internal/cfgnorm"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	metricsnoop "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	"github.com/primandproper/platform-go/v10/observability/metrics/otelgrpc"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderOtel represents the open source tracing server.
	ProviderOtel = "otelgrpc"
	// ProviderNoop, and the empty string, select no metrics at all. That is the
	// deliberate opt-out and stays supported; what is no longer supported is a
	// provider name this package does not recognize, which used to disable
	// metrics silently and looked exactly like the opt-out.
	ProviderNoop = "noop"
)

// providers are every provider this package implements, plus the empty string,
// which selects no metrics — the deliberate opt-out. Validation and
// NewMetricsProvider both read it.
var providers = []string{"", ProviderNoop, ProviderOtel}

type (
	// Config contains settings related to tracing.
	Config struct {
		_ struct{} `json:"-" yaml:"-"`

		Otel        *otelgrpc.Config `env:",init"        envPrefix:"OTEL_"            json:"otelgrpc,omitempty"    yaml:"otelgrpc,omitempty"`
		ServiceName string           `env:"SERVICE_NAME" json:"serviceName,omitempty" yaml:"serviceName,omitempty"`
		Provider    string           `env:"PROVIDER"     json:"provider,omitempty"    yaml:"provider,omitempty"`
		Enabled     bool             `env:"ENABLED"      json:"enabled,omitempty"     yaml:"enabled,omitempty"`
	}
)

// NewMetricsProvider provides a metrics provider.
func (c *Config) NewMetricsProvider(ctx context.Context, opts ...Option) (metrics.Provider, error) {
	// EnsureLogger, not the raw option: the logger is optional now, and the
	// otelgrpc provider logs what it set up.
	logger := logging.EnsureLogger(newOptions(opts).logger)

	if c == nil {
		return nil, errors.ErrNilInputParameter
	}

	provider, err := cfgnorm.SelectProvider(c.Provider, providers, "metrics provider")
	if err != nil {
		return nil, err
	}

	if err = c.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating metrics config")
	}

	// Checked after validation rather than before: a config that is off but
	// wrong is still wrong, and finding out about it when someone turns metrics
	// on is finding out at the worst moment.
	if !c.Enabled {
		return metricsnoop.NewMetricsProvider(), nil
	}

	switch provider {
	case ProviderOtel:
		return otelgrpc.NewMetricsProvider(ctx, logger, c.ServiceName, c.Otel)
	case "", ProviderNoop:
		return metricsnoop.NewMetricsProvider(), nil
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "metrics provider %q", c.Provider)
	}
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// ValidateWithContext validates the config struct.
func (c *Config) ValidateWithContext(ctx context.Context) error {
	// Release the sub-configs env parsing's ",init" allocated and nothing filled
	// in, so the Nil rules below read "the operator configured this" rather than
	// "env parsing ran".
	cfgnorm.ZeroToNil(&c.Otel)

	provider := cfgnorm.Provider(c.Provider)

	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Provider, validation.By(func(any) error {
			// Checked normalized, matching dispatch: validating the raw string
			// rejected "OtelGRPC" while NewMetricsProvider built it. Checked
			// whether or not metrics are Enabled, because a name nobody
			// recognizes is a mistake in either state, and the state it is
			// found in is not the state it will be discovered in.
			if !slices.Contains(providers, provider) {
				return errors.Wrapf(errors.ErrUnknownProvider, "metrics provider %q", c.Provider)
			}

			return nil
		})),
		validation.Field(&c.Otel, validation.When(c.Enabled && provider == ProviderOtel, validation.Required).Else(validation.Nil)),
	)
}
