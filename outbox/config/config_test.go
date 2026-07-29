package outboxcfg

import (
	"testing"

	"github.com/primandproper/platform-go/v8/database/dialect"
	databasemock "github.com/primandproper/platform-go/v8/database/mock"
	loggingnoop "github.com/primandproper/platform-go/v8/observability/logging/noop"
	"github.com/primandproper/platform-go/v8/outbox"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// sqliteConfig is a valid in-process configuration: SQLite dialect, noop
// publisher.
func sqliteConfig() *Config {
	return &Config{
		Relay: outbox.RelayConfig{Dialect: dialect.SQLite},
	}
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills in zero fields", func(t *testing.T) {
		t.Parallel()

		cfg := sqliteConfig()
		cfg.EnsureDefaults()

		test.EqOp(t, outbox.DefaultTableName, cfg.Relay.TableName)
		test.EqOp(t, outbox.DefaultBatchSize, cfg.Relay.BatchSize)
	})

	T.Run("leaves set fields alone", func(t *testing.T) {
		t.Parallel()

		cfg := sqliteConfig()
		cfg.Relay.TableName = "custom_outbox"
		cfg.EnsureDefaults()

		test.EqOp(t, "custom_outbox", cfg.Relay.TableName)
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
}

func TestNewRelay(T *testing.T) {
	T.Parallel()

	T.Run("builds a relay with a noop publisher by default", func(t *testing.T) {
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
}
