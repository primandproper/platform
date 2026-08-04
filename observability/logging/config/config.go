package loggingcfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/internal/cfgnorm"
	"github.com/primandproper/platform-go/v9/observability/logging"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	"github.com/primandproper/platform-go/v9/observability/logging/otelgrpc"
	"github.com/primandproper/platform-go/v9/observability/logging/slog"
	"github.com/primandproper/platform-go/v9/observability/logging/zap"
	"github.com/primandproper/platform-go/v9/observability/logging/zerolog"

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

	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ServiceName, validation.Required),
		validation.Field(&cfg.Level, validation.By(validateLevel)),
		validation.Field(&cfg.Provider, validation.In("", ProviderNoop, ProviderZerolog, ProviderZap, ProviderSlog, ProviderOtelSlog)),
		validation.Field(&cfg.OtelSlog, validation.Skip.When(cfg.Provider != ProviderOtelSlog), validation.Required),
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
	switch strings.TrimSpace(strings.ToLower(cfg.Provider)) {
	case ProviderZerolog:
		logger = zerolog.NewZerologLogger(cfg.Level)
	case ProviderZap:
		logger = zap.NewZapLogger(cfg.Level)
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
