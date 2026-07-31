package auditcfg

import (
	"context"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/audit"
	"github.com/primandproper/platform-go/v8/database"
	"github.com/primandproper/platform-go/v8/database/dialect"
	"github.com/primandproper/platform-go/v8/errors"
	noopLogging "github.com/primandproper/platform-go/v8/observability/logging/noop"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	noopTracing "github.com/primandproper/platform-go/v8/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func validConfig() *Config {
	return &Config{Sweeper: audit.SweeperConfig{Dialect: dialect.SQLite}}
}

func TestConfig(T *testing.T) {
	T.Parallel()

	T.Run("EnsureDefaults reaches the nested config", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, audit.DefaultTablePrefix, cfg.Sweeper.TablePrefix)
		test.EqOp(t, audit.DefaultRetention, cfg.Sweeper.Retention)
	})

	T.Run("ValidateWithContext reaches the nested config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Sweeper: audit.SweeperConfig{Dialect: "cassandra"}}
		cfg.EnsureDefaults()

		// ozzo dereferences a struct-value field before checking
		// ValidatableWithContext, so this only passes because of the By closure.
		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewRecorder(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		recorder, err := NewRecorder(t.Context(), validConfig(), nil, nil, nil)
		must.NoError(t, err)
		test.NotNil(t, recorder)
	})

	T.Run("registers the configured redactions", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Redactions = map[string]audit.Redaction{
			"user": {Drop: []string{"passwordHash"}},
		}

		recorder, err := NewRecorder(t.Context(), cfg, noopLogging.NewLogger(), nil, nil)
		must.NoError(t, err)
		test.NotNil(t, recorder)
	})

	T.Run("passes the observability dependencies through", func(t *testing.T) {
		t.Parallel()

		recorder, err := NewRecorder(
			t.Context(), validConfig(),
			noopLogging.NewLogger(),
			noopTracing.NewTracerProvider(),
			metrics.EnsureMetricsProvider(nil),
		)
		must.NoError(t, err)
		test.NotNil(t, recorder)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewRecorder(t.Context(), nil, nil, nil, nil)
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
	})

	T.Run("rejects an invalid config", func(t *testing.T) {
		t.Parallel()

		_, err := NewRecorder(t.Context(), &Config{}, nil, nil, nil)
		test.Error(t, err)
	})
}

func TestNewReader(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		reader, err := NewReader(t.Context(), validConfig(), nil, nil, nil, stubClient{})
		must.NoError(t, err)
		test.NotNil(t, reader)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewReader(t.Context(), nil, nil, nil, nil, stubClient{})
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
	})

	T.Run("rejects a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewReader(t.Context(), validConfig(), nil, nil, nil, nil)
		test.ErrorIs(t, err, audit.ErrNilDatabaseClient)
	})

	T.Run("passes the observability dependencies through", func(t *testing.T) {
		t.Parallel()

		reader, err := NewReader(
			t.Context(), validConfig(),
			noopLogging.NewLogger(),
			noopTracing.NewTracerProvider(),
			metrics.EnsureMetricsProvider(nil),
			stubClient{},
		)
		must.NoError(t, err)
		test.NotNil(t, reader)
	})
}

func TestNewSweeper(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		sweeper, err := NewSweeper(t.Context(), validConfig(), nil, nil, nil, stubClient{})
		must.NoError(t, err)
		test.NotNil(t, sweeper)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewSweeper(t.Context(), nil, nil, nil, nil, stubClient{})
		test.ErrorIs(t, err, errors.ErrNilInputParameter)
	})

	T.Run("rejects a nil client", func(t *testing.T) {
		t.Parallel()

		_, err := NewSweeper(t.Context(), validConfig(), nil, nil, nil, nil)
		test.ErrorIs(t, err, audit.ErrNilDatabaseClient)
	})

	T.Run("passes the observability dependencies through", func(t *testing.T) {
		t.Parallel()

		sweeper, err := NewSweeper(
			t.Context(), validConfig(),
			noopLogging.NewLogger(),
			noopTracing.NewTracerProvider(),
			metrics.EnsureMetricsProvider(nil),
			stubClient{},
		)
		must.NoError(t, err)
		test.NotNil(t, sweeper)
	})
}

// stubClient satisfies database.Client for the constructors, which only hold
// onto it. Every method panics, so a constructor that started issuing queries
// would fail loudly rather than be covered by accident.
type stubClient struct{}

var _ database.Client = (*stubClient)(nil)

func (stubClient) Reader() database.SQLQueryExecutor { panic("unexpected read") }
func (stubClient) Writer() database.SQLQueryExecutor { panic("unexpected write") }

func (stubClient) WithTransaction(context.Context, func(database.SQLQueryExecutor) error) error {
	panic("unexpected transaction")
}

func (stubClient) Close() error           { return nil }
func (stubClient) CurrentTime() time.Time { panic("unexpected clock read") }
