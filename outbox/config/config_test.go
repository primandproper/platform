package outboxcfg

import (
	"testing"

	"github.com/primandproper/platform-go/v9/database/dialect"
	databasemock "github.com/primandproper/platform-go/v9/database/mock"
	msgconfig "github.com/primandproper/platform-go/v9/messagequeue/config"
	"github.com/primandproper/platform-go/v9/messagequeue/pubsub"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	metricsnoop "github.com/primandproper/platform-go/v9/observability/metrics/noop"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"
	"github.com/primandproper/platform-go/v9/outbox"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// sqliteConfig is a valid in-process configuration: SQLite dialect, noop
// publisher.
func sqliteConfig() *Config {
	return &Config{
		Relay: outbox.RelayConfig{Dialect: dialect.SQLite},
		Queue: msgconfig.Config{Publisher: msgconfig.MessageQueueConfig{Provider: msgconfig.ProviderNoop}},
	}
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := sqliteConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, outbox.DefaultTablePrefix, cfg.Relay.TablePrefix)
		test.EqOp(t, outbox.DefaultBatchSize, cfg.Relay.BatchSize)
	})

	T.Run("leaves set fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := sqliteConfig()
		cfg.Relay.TablePrefix = "custom_outbox"
		cfg.EnsureDefaults()

		test.EqOp(t, "custom_outbox", cfg.Relay.TablePrefix)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a valid config", func(t *testing.T) {
		t.Parallel()

		cfg := sqliteConfig()
		cfg.EnsureDefaults()

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	// The nested config is reached through a validation.By closure, because
	// ozzo would otherwise dereference the struct and skip it.
	T.Run("rejects an invalid nested relay config", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestNewWriter(T *testing.T) {
	T.Parallel()

	T.Run("builds a writer from the relay section", func(t *testing.T) {
		t.Parallel()

		w, err := NewWriter(t.Context(), sqliteConfig(), loggingnoop.NewLogger(), nil, nil)
		must.NoError(t, err)
		must.NotNil(t, w)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewWriter(t.Context(), nil, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects a config without a dialect", func(t *testing.T) {
		t.Parallel()

		_, err := NewWriter(t.Context(), &Config{}, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("derives options from every observability argument", func(t *testing.T) {
		t.Parallel()

		w, err := NewWriter(
			t.Context(),
			sqliteConfig(),
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
			metricsnoop.NewMetricsProvider(),
		)
		must.NoError(t, err)
		must.NotNil(t, w)
	})

	T.Run("explicit options run after the config-derived ones", func(t *testing.T) {
		t.Parallel()

		w, err := NewWriter(
			t.Context(),
			sqliteConfig(),
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
			metricsnoop.NewMetricsProvider(),
			outbox.WithWriterTablePrefix("override_table"),
		)
		must.NoError(t, err)
		must.NotNil(t, w)
	})
}

func TestNewRelay(T *testing.T) {
	T.Parallel()

	T.Run("builds a relay with a noop publisher", func(t *testing.T) {
		t.Parallel()

		r, err := NewRelay(t.Context(), sqliteConfig(), loggingnoop.NewLogger(), nil, nil, &databasemock.ClientMock{})
		must.NoError(t, err)
		must.NotNil(t, r)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewRelay(t.Context(), nil, loggingnoop.NewLogger(), nil, nil, &databasemock.ClientMock{})
		test.Error(t, err)
	})

	T.Run("rejects a nil database client", func(t *testing.T) {
		t.Parallel()

		_, err := NewRelay(t.Context(), sqliteConfig(), loggingnoop.NewLogger(), nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects a config without a dialect", func(t *testing.T) {
		t.Parallel()

		_, err := NewRelay(t.Context(), &Config{}, loggingnoop.NewLogger(), nil, nil, &databasemock.ClientMock{})
		test.Error(t, err)
	})

	T.Run("surfaces a publisher provider that will not build", func(t *testing.T) {
		t.Parallel()

		// PubSub with no project ID fails client construction, which is the
		// cheapest way to make the provider step fail without a network.
		cfg := sqliteConfig()
		cfg.Queue = msgconfig.Config{
			Publisher: msgconfig.MessageQueueConfig{
				Provider: msgconfig.ProviderPubSub,
				PubSub:   pubsub.Config{},
			},
		}

		r, err := NewRelay(t.Context(), cfg, loggingnoop.NewLogger(), nil, nil, &databasemock.ClientMock{})
		test.Nil(t, r)
		test.Error(t, err)
	})

	T.Run("derives options from every observability argument", func(t *testing.T) {
		t.Parallel()

		r, err := NewRelay(
			t.Context(),
			sqliteConfig(),
			loggingnoop.NewLogger(),
			tracingnoop.NewTracerProvider(),
			metricsnoop.NewMetricsProvider(),
			&databasemock.ClientMock{},
		)
		must.NoError(t, err)
		must.NotNil(t, r)
	})
}
