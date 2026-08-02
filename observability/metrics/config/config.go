package metricscfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	metricsnoop "github.com/primandproper/platform-go/v9/observability/metrics/noop"
	"github.com/primandproper/platform-go/v9/observability/metrics/otelgrpc"

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

type (
	// Config contains settings related to tracing.
	Config struct {
		_ struct{} `json:"-" yaml:"-"`

		Otel        *otelgrpc.Config `env:",init"        envPrefix:"OTEL_"         json:"otelgrpc,omitempty" yaml:"otelgrpc,omitempty"`
		ServiceName string           `env:"SERVICE_NAME" json:"serviceName"        yaml:"serviceName"`
		Provider    string           `env:"PROVIDER"     json:"provider,omitempty" yaml:"provider,omitempty"`
		Enabled     bool             `env:"ENABLED"      json:"enabled"            yaml:"enabled"`
	}
)

// NewMetricsProvider provides a metrics provider.
func (c *Config) NewMetricsProvider(ctx context.Context, opts ...Option) (metrics.Provider, error) {
	// EnsureLogger, not the raw option: the logger is optional now, and the
	// otelgrpc provider logs what it set up.
	logger := logging.EnsureLogger(newOptions(opts).logger)

	if !c.Enabled {
		return metricsnoop.NewMetricsProvider(), nil
	}

	switch strings.TrimSpace(strings.ToLower(c.Provider)) {
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
	return validation.ValidateStructWithContext(ctx, c,
		validation.Field(&c.Provider, validation.When(c.Enabled, validation.In("", ProviderNoop, ProviderOtel))),
		validation.Field(&c.Otel, validation.When(c.Enabled && c.Provider == ProviderOtel, validation.Required).Else(validation.Nil)),
	)
}
