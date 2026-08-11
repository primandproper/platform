package loggingcfg

import (
	"context"
	"slices"

	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/internal/cfgnorm"
	"github.com/primandproper/platform-go/v10/observability/logging"
	loggingnoop "github.com/primandproper/platform-go/v10/observability/logging/noop"
	"github.com/primandproper/platform-go/v10/observability/logging/otelgrpc"
	"github.com/primandproper/platform-go/v10/observability/logging/slog"
	"github.com/primandproper/platform-go/v10/observability/logging/zap"
	"github.com/primandproper/platform-go/v10/observability/logging/zerolog"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

const (
	// ProviderZerolog indicates you'd like to use the zerolog logger.
	ProviderZerolog = "zerolog"
	// ProviderZap indicates you'd like to use the zap logger.
	ProviderZap = "zap"
	// ProviderSlog indicates you'd like to use the slog logger.
	ProviderSlog = "slog"
	// ProviderOtelSlog indicates you'd like to use the otel-enabled slog logger.
	ProviderOtelSlog = "otelslog"
	// ProviderNoop, and the empty string, select no logging at all. That is the
	// deliberate opt-out and stays supported; what is no longer supported is a
	// provider name this package does not recognize, which used to disable
	// logging silently and looked exactly like the opt-out.
	ProviderNoop = "noop"
)

// providers are every provider this package implements, plus the empty string,
// which selects the noop logger — the deliberate opt-out. Validation and
// NewLogger both read it.
var providers = []string{"", ProviderNoop, ProviderZerolog, ProviderZap, ProviderSlog, ProviderOtelSlog}

type (
	// Config configures a Logger.
	Config struct {
		_ struct{} `json:"-" yaml:"-"`

		ServiceName string           `env:"SERVICE_NAME" json:"serviceName,omitempty" yaml:"serviceName,omitempty"`
		Level       logging.Level    `env:"LEVEL"        json:"level,omitempty"       yaml:"level,omitempty"`
		OtelSlog    *otelgrpc.Config `env:",init"        envPrefix:"OTEL_SLOG_"       json:"otelslog,omitempty"    yaml:"otelslog,omitempty"`
		Provider    string           `env:"PROVIDER"     json:"provider,omitempty"    yaml:"provider,omitempty"`
	}
)

// ValidateWithContext validates the config struct.
//
// The sub-config for a provider that was not selected is skipped rather than
// merely unguarded: ozzo validates any non-nil pointer to a Validatable once a
// field's rules have run, and `env:",init"` leaves every sub-config non-nil. A
// validation.When guard alone stops the Required rule and nothing else, so the
// unselected provider's own rules were enforced and a service logging with slog
// could not load.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	// Release the sub-config env parsing's ",init" allocated and nothing filled
	// in, so the rule below reads "the operator configured this" rather than
	// "env parsing ran". Without it a logger naming no provider at all fails
	// validation on an otelslog endpoint nobody asked for, which is every
	// config that has been through env.Parse. The three sibling pillars each
	// do the same for theirs.
	cfgnorm.ZeroToNil(&cfg.OtelSlog)

	provider := cfgnorm.Provider(cfg.Provider)

	return validation.ValidateStructWithContext(ctx, cfg,
		// Required only for the provider that sends it anywhere. It was
		// unconditional, and unreachable, until NewLogger started calling this;
		// reachable and unconditional would have made a service that logs to
		// stdout with zerolog name a service to nobody.
		validation.Field(&cfg.ServiceName, validation.Required.When(provider == ProviderOtelSlog)),
		validation.Field(&cfg.Level, validation.By(validateLevel)),
		validation.Field(&cfg.Provider, validation.By(func(any) error {
			// Checked normalized, matching dispatch: validating the raw string
			// rejected "Zerolog" and " slog " while NewLogger built them.
			if !slices.Contains(providers, provider) {
				return errors.Wrapf(errors.ErrUnknownProvider, "logging provider %q", cfg.Provider)
			}

			return nil
		})),
		validation.Field(&cfg.OtelSlog, validation.Skip.When(provider != ProviderOtelSlog), validation.Required),
	)
}

// validateLevel accepts the zero Level — which every implementation reads as
// InfoLevel — or one of the known levels.
func validateLevel(value any) error {
	lvl, ok := value.(logging.Level)
	if !ok || lvl == "" || lvl.Valid() {
		return nil
	}

	return validation.NewError("validation_invalid_log_level", "must be a valid log level")
}

// NewLogger builds a logger according to the provided config.
func (cfg *Config) NewLogger(ctx context.Context) (logger logging.Logger, err error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	provider, err := cfgnorm.SelectProvider(cfg.Provider, providers, "logging provider")
	if err != nil {
		return nil, err
	}

	if err = cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating logging config")
	}

	switch provider {
	case ProviderZerolog:
		logger = zerolog.NewZerologLogger(cfg.Level)
	case ProviderZap:
		logger, err = zap.NewZapLogger(cfg.Level)
	case ProviderSlog:
		logger = slog.NewSlogLogger(cfg.Level)
	case ProviderOtelSlog:
		logger, err = otelgrpc.NewOtelSlogLogger(ctx, cfg.Level, cfg.ServiceName, cfg.OtelSlog)
	case "", ProviderNoop:
		logger = loggingnoop.NewLogger()
	default:
		return nil, errors.Wrapf(errors.ErrUnknownProvider, "logging provider %q", cfg.Provider)
	}

	return logger, err
}
