/*
Package outboxcfg assembles the outbox from environment configuration: a Writer
for the transactional side and a Relay, with its publisher provider, for the
delivery side.

Both halves read the same Relay section, so the dialect and table the Writer
writes to are by construction the ones the Relay claims from. The queue section
selects the messagequeue provider the Relay publishes through, exactly as
messagequeue/config would configure it standalone.
*/
package outboxcfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/errors"
	messagequeuecfg "github.com/primandproper/platform-go/v9/messagequeue/config"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	"github.com/primandproper/platform-go/v9/outbox"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config assembles an outbox Writer and Relay from environment configuration.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// Queue configures the publisher provider the Relay hands claimed messages
	// to. An empty provider selects the noop publisher, which is right for
	// tests and wrong for production: messages would be claimed, "published"
	// nowhere, and marked done.
	Queue messagequeuecfg.Config `env:",init" envPrefix:"QUEUE_" json:"queue" yaml:"queue"`

	// Relay carries the outbox's own knobs. Its Dialect and TableName also
	// drive the Writer, so the writing and claiming halves cannot disagree
	// about which table the outbox lives in.
	Relay outbox.RelayConfig `env:",init" json:"relay" yaml:"relay"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
func (cfg *Config) EnsureDefaults() {
	cfg.Relay.EnsureDefaults()
}

// ValidateWithContext validates a Config struct.
//
// The nested config is validated through a validation.By closure because ozzo
// dereferences a struct-value field before checking ValidatableWithContext, so
// it would otherwise be skipped.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Relay, validation.By(func(any) error {
			return cfg.Relay.ValidateWithContext(ctx)
		})),
	)
}

// NewWriter builds a Writer from configuration. The table comes from the Relay
// section; the dialect comes from client, which must be the database holding
// the outbox table — the same one the Writer's transactions run against.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything.
func NewWriter(
	ctx context.Context,
	cfg *Config,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	client database.Client,
	opts ...outbox.WriterOption,
) (*outbox.Writer, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	if client == nil {
		return nil, outbox.ErrNilDatabaseClient
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating outbox config")
	}

	base := []outbox.WriterOption{outbox.WithWriterTablePrefix(cfg.Relay.TablePrefix)}
	if logger != nil {
		base = append(base, outbox.WithWriterLogger(logger))
	}
	if tracerProvider != nil {
		base = append(base, outbox.WithWriterTracerProvider(tracerProvider))
	}
	if metricsProvider != nil {
		base = append(base, outbox.WithWriterMetricsProvider(metricsProvider))
	}

	return outbox.NewWriter(client.Dialect(), append(base, opts...)...)
}

// NewRelay builds a Relay from configuration, including the publisher provider
// it delivers through. client must be the database holding the outbox table —
// the same one the Writer's transactions run against.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything.
func NewRelay(
	ctx context.Context,
	cfg *Config,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	client database.Client,
	opts ...outbox.RelayOption,
) (*outbox.Relay, error) {
	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating outbox config")
	}

	provider, err := messagequeuecfg.NewPublisherProvider(ctx, logger, tracerProvider, metricsProvider, &cfg.Queue)
	if err != nil {
		return nil, errors.Wrap(err, "building outbox publisher provider")
	}

	var base []outbox.RelayOption
	if logger != nil {
		base = append(base, outbox.WithRelayLogger(logger))
	}
	if tracerProvider != nil {
		base = append(base, outbox.WithRelayTracerProvider(tracerProvider))
	}
	if metricsProvider != nil {
		base = append(base, outbox.WithRelayMetricsProvider(metricsProvider))
	}

	return outbox.NewRelay(ctx, &cfg.Relay, client, provider, append(base, opts...)...)
}
