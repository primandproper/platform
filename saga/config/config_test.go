package sagacfg

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/database/sqlite"
	"github.com/primandproper/platform-go/v9/distributedlock"
	lockmemory "github.com/primandproper/platform-go/v9/distributedlock/memory"
	"github.com/primandproper/platform-go/v9/saga"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// testClientConfig is the minimum database.ClientConfig a SQLite client needs.
type testClientConfig struct {
	connectionString string
}

var _ database.ClientConfig = (*testClientConfig)(nil)

func (c *testClientConfig) GetReadConnectionString() string   { return c.connectionString }
func (c *testClientConfig) GetWriteConnectionString() string  { return c.connectionString }
func (c *testClientConfig) GetMaxPingAttempts() uint64        { return 1 }
func (c *testClientConfig) GetPingWaitPeriod() time.Duration  { return time.Millisecond }
func (c *testClientConfig) GetMaxIdleConns() int              { return 2 }
func (c *testClientConfig) GetMaxOpenConns() int              { return 1 }
func (c *testClientConfig) GetConnMaxLifetime() time.Duration { return time.Minute }

func newClient(t *testing.T) database.Client {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "saga.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func newLocker(t *testing.T) distributedlock.ScopedLocker {
	t.Helper()

	raw, err := lockmemory.NewLocker()
	must.NoError(t, err)

	scoped, err := distributedlock.NewScopedLocker(raw)
	must.NoError(t, err)

	return scoped
}

func validConfig() *Config {
	cfg := &Config{Dialect: dialect.SQLite}
	cfg.EnsureDefaults()

	return cfg
}

func TestConfig_EnsureDefaults(T *testing.T) {
	T.Parallel()

	T.Run("fills the prefix, the topic, and the worker", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.SQLite}
		cfg.EnsureDefaults()

		test.EqOp(t, saga.DefaultTablePrefix, cfg.TablePrefix)
		test.EqOp(t, saga.DefaultEventTopic, cfg.EventTopic)
		test.EqOp(t, saga.DefaultPollInterval, cfg.Worker.PollInterval)
	})

	T.Run("leaves set values alone", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{
			Dialect:     dialect.Postgres,
			TablePrefix: "app_saga",
			EventTopic:  "sagas",
		}
		cfg.EnsureDefaults()

		test.EqOp(t, "app_saga", cfg.TablePrefix)
		test.EqOp(t, "sagas", cfg.EventTopic)
	})
}

func TestConfig_ValidateWithContext(T *testing.T) {
	T.Parallel()

	T.Run("accepts a defaulted config", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, validConfig().ValidateWithContext(t.Context()))
	})

	T.Run("rejects a missing dialect", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{}
		cfg.EnsureDefaults()

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an unsupported dialect", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Dialect("oracle")}
		cfg.EnsureDefaults()

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)

		// ozzo collects field errors into a map that does not unwrap, so the
		// assertion is on the rendering rather than on errors.Is.
		test.StrContains(t, err.Error(), "unsupported SQL dialect")
	})

	T.Run("rejects a worker config that cannot be satisfied", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Worker.LeaseDuration = time.Second

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("rejects an empty table prefix", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.TablePrefix = ""

		test.Error(t, cfg.ValidateWithContext(t.Context()))
	})
}

func TestProvideStore(T *testing.T) {
	T.Parallel()

	T.Run("builds a store", func(t *testing.T) {
		t.Parallel()

		store, err := ProvideStore(t.Context(), validConfig(), nil, nil, nil, newClient(t))
		must.NoError(t, err)
		must.NotNil(t, store)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := ProvideStore(t.Context(), nil, nil, nil, nil, newClient(t))
		test.Error(t, err)
	})

	T.Run("rejects an invalid config", func(t *testing.T) {
		t.Parallel()

		cfg := validConfig()
		cfg.Dialect = dialect.Dialect("oracle")

		_, err := ProvideStore(t.Context(), cfg, nil, nil, nil, newClient(t))
		test.Error(t, err)
	})

	T.Run("propagates a store construction failure", func(t *testing.T) {
		t.Parallel()

		_, err := ProvideStore(t.Context(), validConfig(), nil, nil, nil, nil)
		test.ErrorIs(t, err, saga.ErrNilDatabaseClient)
	})
}

func TestProvideWorker(T *testing.T) {
	T.Parallel()

	registry := func(t *testing.T) *saga.Registry {
		t.Helper()

		r := saga.NewRegistry()
		must.NoError(t, saga.Register(r, saga.Definition[struct{}]{
			Name: "orders",
			Steps: []saga.Step[struct{}]{{
				Name: "one",
				Do:   func(_ context.Context, _ *struct{}) error { return nil },
			}},
		}))

		return r
	}

	T.Run("builds a worker", func(t *testing.T) {
		t.Parallel()

		store, err := ProvideStore(t.Context(), validConfig(), nil, nil, nil, newClient(t))
		must.NoError(t, err)

		worker, err := ProvideWorker(t.Context(), validConfig(), nil, nil, nil,
			store, registry(t), newLocker(t), nil, nil)
		must.NoError(t, err)
		must.NotNil(t, worker)
	})

	T.Run("rejects a nil config", func(t *testing.T) {
		t.Parallel()

		store, err := ProvideStore(t.Context(), validConfig(), nil, nil, nil, newClient(t))
		must.NoError(t, err)

		_, err = ProvideWorker(t.Context(), nil, nil, nil, nil, store, registry(t), newLocker(t), nil, nil)
		test.Error(t, err)
	})

	T.Run("rejects an invalid config", func(t *testing.T) {
		t.Parallel()

		store, err := ProvideStore(t.Context(), validConfig(), nil, nil, nil, newClient(t))
		must.NoError(t, err)

		cfg := validConfig()
		cfg.Dialect = dialect.Dialect("oracle")

		_, err = ProvideWorker(t.Context(), cfg, nil, nil, nil, store, registry(t), newLocker(t), nil, nil)
		test.Error(t, err)
	})

	T.Run("propagates a missing locker", func(t *testing.T) {
		t.Parallel()

		store, err := ProvideStore(t.Context(), validConfig(), nil, nil, nil, newClient(t))
		must.NoError(t, err)

		_, err = ProvideWorker(t.Context(), validConfig(), nil, nil, nil,
			store, registry(t), nil, nil, nil)
		test.ErrorIs(t, err, saga.ErrNilLocker)
	})
}
