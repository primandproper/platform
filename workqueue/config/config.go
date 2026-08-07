/*
Package workqueuecfg assembles a work queue from environment configuration.

There is one thing to build and one dependency to build it from: a
database.Client speaking Postgres. The dialect is not configured here at all —
it comes off the client, so the SQL cannot disagree with the database it runs
against.

NewQueue is generic over the key type, which the caller names at the call site.
That is the only part of a queue the environment cannot express: a key is a Go
type, so a config file has nothing to say about it.
*/
package workqueuecfg

import (
	"context"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/workqueue"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// Config assembles a workqueue.Queue from environment configuration.
type Config struct {
	_ struct{} `json:"-" yaml:"-"`

	// Queue carries the queue's own knobs, including the name that partitions
	// the shared table and the prefix that names it.
	Queue workqueue.Config `env:",init" json:"queue,omitzero" yaml:"queue,omitempty"`
}

var _ validation.ValidatableWithContext = (*Config)(nil)

// EnsureDefaults fills in zero fields.
func (cfg *Config) EnsureDefaults() {
	cfg.Queue.EnsureDefaults()
}

// ValidateWithContext validates a Config struct.
//
// The nested config is validated through a validation.By closure because ozzo
// dereferences a struct-value field before checking ValidatableWithContext, so
// it would otherwise be skipped.
func (cfg *Config) ValidateWithContext(ctx context.Context) error {
	return validation.ValidateStructWithContext(ctx, cfg,
		validation.Field(&cfg.Queue, validation.By(func(any) error {
			return cfg.Queue.ValidateWithContext(ctx)
		})),
	)
}

// NewQueue builds a Queue from configuration.
//
// K is the key type the queue schedules work for, and is the caller's to name:
//
//	queue, err := workqueuecfg.NewQueue[OrderID](ctx, cfg, client)
//
// Explicit options run after the config-derived ones, so a caller can still
// override anything — including the key codec, which configuration has no way to
// express.
//
// The returned Queue owns a goroutine and must be Closed.
func NewQueue[K comparable](
	ctx context.Context,
	cfg *Config,
	client database.Client,
	opts ...Option,
) (*workqueue.Queue[K], error) {
	o := newOptions(opts)

	if cfg == nil {
		return nil, errors.ErrNilInputParameter
	}

	if client == nil {
		return nil, workqueue.ErrNilDatabaseClient
	}

	cfg.EnsureDefaults()

	if err := cfg.ValidateWithContext(ctx); err != nil {
		return nil, errors.Wrap(err, "validating work queue config")
	}

	base := make([]workqueue.Option, 0, len(o.queue)+3) //nolint:mnd // the three observability options below

	if o.logger != nil {
		base = append(base, workqueue.WithLogger(o.logger))
	}
	if o.tracerProvider != nil {
		base = append(base, workqueue.WithTracerProvider(o.tracerProvider))
	}
	if o.metricsProvider != nil {
		base = append(base, workqueue.WithMetricsProvider(o.metricsProvider))
	}

	return workqueue.New[K](ctx, &cfg.Queue, client, append(base, o.queue...)...)
}
