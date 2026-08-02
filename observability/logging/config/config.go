package loggingcfg

import (
	"context"
	"strings"

	"github.com/primandproper/platform-go/v9/errors"
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

		ServiceName string           `env:"SERVICE_NAME" json:"serviceName"        yaml:"serviceName"`
		Level       logging.Level    `env:"LEVEL"        json:"level,omitempty"    yaml:"level,omitempty"`
		OtelSlog    *otelgrpc.Config `env:",init"         envPrefix:"OTEL_SLOG_"    json:"otelslog,omitempty" yaml:"otelslog,omitempty"`
		Provider    string           `env:"PROVIDER"     json:"provider,omitempty" yaml:"provider,omitempty"`
	}
)

func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.ServiceName, validation.Required),
		validation.Field(&cfg.Level, validation.By(validateLevel)),
		validation.Field(&cfg.Provider, validation.In("", ProviderNoop, ProviderZerolog, ProviderZap, ProviderSlog, ProviderOtelSlog)),
		validation.Field(&cfg.OtelSlog, validation.When(cfg.Provider == ProviderOtelSlog, validation.Required)),
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
