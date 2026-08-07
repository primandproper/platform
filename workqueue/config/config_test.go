package workqueuecfg

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	databasemock "github.com/primandproper/platform-go/v9/database/mock"
	"github.com/primandproper/platform-go/v9/workqueue"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// clientFor is a database.Client that reports one dialect and nothing else.
// NewQueue reads the dialect off it and never touches the pools.
func clientFor(d dialect.Dialect) database.Client {
	return &databasemock.ClientMock{
		DialectFunc: func() dialect.Dialect { return d },
	}
}

func validConfig() *Config {
	return &Config{Queue: workqueue.Config{Name: "jobs"}}
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills the nested section's zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, workqueue.DefaultRetention, cfg.Queue.Retention)
		test.EqOp(t, workqueue.DefaultMaxClaimBatch, cfg.Queue.MaxClaimBatch)
	})

	T.Run("leaves set fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Queue.Retention = time.Hour
		cfg.EnsureDefaults()

		test.EqOp(t, time.Hour, cfg.Queue.Retention)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a valid config", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// The nested config is reached through a validation.By closure, because
	// ozzo would otherwise dereference the struct and skip it.
	T.Run("rejects an invalid nested queue config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewQueue(T *testing.T) {
	T.Parallel()

	T.Run("builds a queue from configuration", func(t *testing.T) {
		t.Parallel()

		q, err := NewQueue[string](t.Context(), validConfig(), clientFor(dialect.Postgres))
		must.NoError(t, err)
		must.NotNil(t, q)
		t.Cleanup(func() { _ = q.Close(t.Context()) })

		test.EqOp(t, "jobs", q.Name())
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewQueue[string](t.Context(), nil, clientFor(dialect.Postgres))
		test.Error(t, err)
	})

	T.Run("rejects a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewQueue[string](t.Context(), validConfig(), nil)
		test.ErrorIs(t, err, workqueue.ErrNilDatabaseClient)
	})

	T.Run("surfaces the dialect the queue refuses", func(t *testing.T) {
		t.Parallel()

		_, err := NewQueue[string](t.Context(), validConfig(), clientFor(dialect.SQLite))
		test.ErrorIs(t, err, dialect.ErrUnsupported)
	})

	T.Run("derives options from every observability argument", func(t *testing.T) {
		t.Parallel()

		q, err := NewQueue[string](t.Context(), validConfig(), clientFor(dialect.Postgres),
			WithPillars(nil),
			WithLogger(nil),
			WithTracerProvider(nil),
			WithMetricsProvider(nil),
			nil,
		)
		must.NoError(t, err)
		t.Cleanup(func() { _ = q.Close(t.Context()) })
	})

	// A codec is a Go value the environment cannot name, so the passthrough is
	// the only way one reaches a queue built from configuration.
	T.Run("passes queue options through", func(t *testing.T) {
		t.Parallel()

		_, err := NewQueue[string](t.Context(), validConfig(), clientFor(dialect.Postgres),
			WithQueueOptions(workqueue.WithKeyCodec(workqueue.DefaultKeyCodec[int]())))

		// The mismatched codec is the proof it arrived: a passthrough that
		// dropped it would build cleanly.
		test.ErrorIs(t, err, workqueue.ErrKeyCodecTypeMismatch)
	})
}
