/*
Package auditcfg assembles the audit log from environment configuration: the
Recorder applications write through, the Reader they query and verify with, and
the Sweeper that enforces retention.

All three read the same Sweeper section for the dialect and table prefix, so the
tables written to are by construction the ones read from and pruned. Getting
those two out of step is the one misconfiguration that would be invisible until
somebody asked the log a question and got an empty answer.
*/
package auditcfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/audit"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/errors"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config assembles an audit Recorder, Reader, and Sweeper from environment
// configuration.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// Redactions declares what happens to named fields on the way into the log,
	// keyed by resource type. The empty key applies to every resource type.
	//
	// It carries no env tag: a map of string slices has no reasonable flat
	// environment encoding, and this is a policy that belongs in a reviewed file
	// rather than in a deployment variable — "which fields must never be
	// recorded" is exactly the sort of decision that should show up in a diff.
	Redactions map[string]audit.Redaction `json:"redactions,omitempty" yaml:"redactions,omitempty"`

	// Sweeper carries the retention knobs, and the dialect and table prefix that
	// the Recorder and Reader take as well.
	Sweeper audit.SweeperConfig `env:",init" json:"sweeper,omitzero" yaml:"sweeper,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
func (cfg *Config) EnsureDefaults() {
	cfg.Sweeper.EnsureDefaults()
}

// ValidateWithContext validates a Config struct.
//
// The nested config is validated through a validation.By closure because ozzo
// dereferences a struct-value field before checking ValidatableWithContext, so
// it would otherwise be skipped.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Sweeper, validation.By(func(any) error {
			return cfg.Sweeper.ValidateWithContext(ctx)
		})),
	)
}

// NewRecorder builds a Recorder from configuration. The dialect and table
// prefix come from the Sweeper section — see Config.Sweeper for why.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything, and can register redactions beyond those in the file — see
// WithRecorderOptions.
func NewRecorder(
	ctx context.Context,
	cfg *Config,
	opts ...Option,
) (audit.Recorder, error) {
	if err := prepare(ctx, cfg); err != nil {
		return nil, err
	}

	o := newOptions(opts)

	base := []audit.RecorderOption{audit.WithRecorderTablePrefix(cfg.Sweeper.TablePrefix)}
	if o.logger != nil {
		base = append(base, audit.WithRecorderLogger(o.logger))
	}
	if o.tracerProvider != nil {
		base = append(base, audit.WithRecorderTracerProvider(o.tracerProvider))
	}
	if o.metricsProvider != nil {
		base = append(base, audit.WithRecorderMetricsProvider(o.metricsProvider))
	}

	for resourceType := range cfg.Redactions {
		base = append(base, audit.WithRedaction(resourceType, cfg.Redactions[resourceType]))
	}

	return audit.NewRecorder(cfg.Sweeper.Dialect, append(base, o.recorder...)...)
}

// NewReader builds a Reader from configuration. client must be the database
// holding the audit tables — the same one the Recorder's transactions run
// against.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything — see WithReaderOptions.
func NewReader(
	ctx context.Context,
	cfg *Config,
	client database.Client,
	opts ...Option,
) (audit.Reader, error) {
	if err := prepare(ctx, cfg); err != nil {
		return nil, err
	}

	o := newOptions(opts)

	base := []audit.ReaderOption{audit.WithReaderTablePrefix(cfg.Sweeper.TablePrefix)}
	if o.logger != nil {
		base = append(base, audit.WithReaderLogger(o.logger))
	}
	if o.tracerProvider != nil {
		base = append(base, audit.WithReaderTracerProvider(o.tracerProvider))
	}
	if o.metricsProvider != nil {
		base = append(base, audit.WithReaderMetricsProvider(o.metricsProvider))
	}

	return audit.NewReader(client, append(base, o.reader...)...)
}

// NewSweeper builds a Sweeper from configuration. It does not start it; call
// Run.
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything — see WithSweeperOptions.
func NewSweeper(
	ctx context.Context,
	cfg *Config,
	client database.Client,
	opts ...Option,
) (*audit.Sweeper, error) {
	if err := prepare(ctx, cfg); err != nil {
		return nil, err
	}

	o := newOptions(opts)

	var base []audit.SweeperOption
	if o.logger != nil {
		base = append(base, audit.WithSweeperLogger(o.logger))
	}
	if o.tracerProvider != nil {
		base = append(base, audit.WithSweeperTracerProvider(o.tracerProvider))
	}
	if o.metricsProvider != nil {
		base = append(base, audit.WithSweeperMetricsProvider(o.metricsProvider))
	}

	return audit.NewSweeper(ctx, &cfg.Sweeper, client, append(base, o.sweeper...)...)
}

// prepare defaults and validates a config, shared by all three constructors so
// that building one component cannot succeed against a config the next one
// would reject.
func prepare(ctx context.Context, cfg *Config) error {
	if cfg == nil {
		return errors.ErrNilInputParameter
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return errors.Wrap(err, "validating audit config")
	}

	return nil
}
