package dataprivacycfg

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/audit"
	"github.com/primandproper/platform-go/v9/database"
	"github.com/primandproper/platform-go/v9/database/dialect"
	"github.com/primandproper/platform-go/v9/database/sqlite"
	"github.com/primandproper/platform-go/v9/dataprivacy"
	"github.com/primandproper/platform-go/v9/dataprivacy/auditerasure"
	"github.com/primandproper/platform-go/v9/dataprivacy/migrations"
	loggingnoop "github.com/primandproper/platform-go/v9/observability/logging/noop"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	tracingnoop "github.com/primandproper/platform-go/v9/observability/tracing/noop"
	"github.com/primandproper/platform-go/v9/uploads/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestConfig(T *testing.T) {
	T.Parallel()

	T.Run("fills defaults", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Postgres}
		cfg.EnsureDefaults()

		test.EqOp(t, dataprivacy.DefaultTablePrefix, cfg.TablePrefix)
		test.EqOp(t, audit.DefaultTablePrefix, cfg.AuditErasure.TablePrefix)
		test.EqOp(t, auditerasure.DefaultRetentionBasis, cfg.AuditErasure.RetentionBasis)

		// The audit eraser is registered unless an operator turns it off: an
		// erasure that silently skipped a store of personal data would be the
		// more surprising default.
		test.False(t, cfg.AuditErasure.Disabled)

		test.NoError(t, cfg.ValidateWithContext(t.Context()))
	})

	T.Run("requires a valid dialect", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Dialect("oracle")}
		cfg.EnsureDefaults()

		// ozzo collects field errors into a map, which does not forward
		// errors.Is to the causes underneath — so this asserts on the rendering.
		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "unsupported SQL dialect")
	})

	T.Run("validates the nested configs", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.Postgres}
		cfg.EnsureDefaults()

		// ozzo dereferences a struct-value field before checking
		// ValidatableWithContext, so these are reached through By closures — a
		// regression here would silently stop validating them.
		cfg.Worker.LeaseDuration = cfg.Worker.FulfillmentTimeout

		err := cfg.ValidateWithContext(t.Context())
		must.Error(t, err)
		test.StrContains(t, err.Error(), "must exceed fulfillment timeout")
	})
}

func TestRegisterAuditEraser(T *testing.T) {
	T.Parallel()

	T.Run("registers by default", func(t *testing.T) {
		t.Parallel()

		registry := dataprivacy.NewRegistry()

		registered, err := RegisterAuditEraser(t.Context(), &Config{Dialect: dialect.SQLite}, registry)
		must.NoError(t, err)

		test.True(t, registered)
		test.Eq(t, []string{auditerasure.DefaultKey}, registry.EraserKeys())
	})

	T.Run("Disabled leaves the audit log untouched", func(t *testing.T) {
		t.Parallel()

		registry := dataprivacy.NewRegistry()

		cfg := &Config{Dialect: dialect.SQLite}
		cfg.AuditErasure.Disabled = true

		registered, err := RegisterAuditEraser(t.Context(), cfg, registry)
		must.NoError(t, err)

		test.False(t, registered)
		test.SliceEmpty(t, registry.EraserKeys())
	})

	T.Run("refuses a nil registry", func(t *testing.T) {
		t.Parallel()

		_, err := RegisterAuditEraser(t.Context(), &Config{Dialect: dialect.SQLite}, nil)
		test.Error(t, err)
	})

	T.Run("propagates a bad audit table prefix", func(t *testing.T) {
		t.Parallel()

		cfg := &Config{Dialect: dialect.SQLite}
		cfg.AuditErasure.TablePrefix = "drop table;--"

		_, err := RegisterAuditEraser(t.Context(), cfg, dataprivacy.NewRegistry())
		test.ErrorIs(t, err, auditerasure.ErrInvalidTablePrefix)
	})
}

func TestEnsurePackaging(T *testing.T) {
	T.Parallel()

	T.Run("supplies nothing when nothing is configured", func(t *testing.T) {
		t.Parallel()

		workerOpts, serviceOpts := EnsurePackaging(nil, nil)

		test.SliceEmpty(t, workerOpts)
		test.SliceEmpty(t, serviceOpts)
	})
}

func TestConstructors(T *testing.T) {
	T.Parallel()

	T.Run("refuse a nil config", func(t *testing.T) {
		t.Parallel()

		_, err := NewStore(t.Context(), nil, nil)
		test.Error(t, err)

		_, err = NewService(t.Context(), nil, nil, nil, nil, nil)
		test.Error(t, err)

		_, err = NewWorker(t.Context(), nil, nil, nil, nil, nil, nil, nil, false)
		test.Error(t, err)

		_, err = NewSweeper(t.Context(), nil, nil, nil, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("assemble every part from one config", func(t *testing.T) {
		t.Parallel()

		env := newConfigEnv(t)
		cfg := &Config{Dialect: dialect.SQLite, TablePrefix: env.prefix}

		store, err := NewStore(t.Context(), cfg, env.client)
		must.NoError(t, err)
		must.NotNil(t, store)

		svc, err := NewService(t.Context(), cfg,
			loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), metrics.EnsureMetricsProvider(nil), store)
		must.NoError(t, err)
		must.NotNil(t, svc)

		registry := dataprivacy.NewRegistry()
		must.NoError(t, registry.RegisterEraser("identity", dataprivacy.EraserFunc(
			func(context.Context, database.SQLQueryExecutor, dataprivacy.Subject) (dataprivacy.ErasureOutcome, error) {
				return dataprivacy.ErasureOutcome{}, nil
			},
		)))

		worker, err := NewWorker(t.Context(), cfg,
			loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), metrics.EnsureMetricsProvider(nil),
			store, registry, nil, false)
		must.NoError(t, err)
		must.NotNil(t, worker)

		sweeper, err := NewSweeper(t.Context(), cfg,
			loggingnoop.NewLogger(), tracingnoop.NewTracerProvider(), metrics.EnsureMetricsProvider(nil),
			store, nil)
		must.NoError(t, err)
		must.NotNil(t, sweeper)

		// The whole point of one Config: the prefix the Service writes to is by
		// construction the one the Worker claims from.
		req, err := svc.Submit(t.Context(), dataprivacy.Subject{ID: "user-1"}, dataprivacy.RequestErasure)
		must.NoError(t, err)

		read, err := svc.Get(t.Context(), req.ID)
		must.NoError(t, err)
		test.EqOp(t, req.ID, read.ID)
	})

	T.Run("propagate a bad dialect", func(t *testing.T) {
		t.Parallel()

		env := newConfigEnv(t)
		cfg := &Config{Dialect: dialect.Dialect("oracle"), TablePrefix: env.prefix}

		_, err := NewStore(t.Context(), cfg, env.client)
		test.Error(t, err)

		_, err = NewService(t.Context(), cfg, nil, nil, nil, nil)
		test.Error(t, err)

		_, err = NewWorker(t.Context(), cfg, nil, nil, nil, nil, dataprivacy.NewRegistry(), nil, false)
		test.Error(t, err)

		_, err = NewSweeper(t.Context(), cfg, nil, nil, nil, nil, nil)
		test.Error(t, err)
	})

	T.Run("a worker with an uploader gets a URL signer", func(t *testing.T) {
		t.Parallel()

		env := newConfigEnv(t)
		cfg := &Config{Dialect: dialect.SQLite, TablePrefix: env.prefix}

		store, err := NewStore(t.Context(), cfg, env.client)
		must.NoError(t, err)

		registry := dataprivacy.NewRegistry()
		must.NoError(t, registry.RegisterCollector("identity", dataprivacy.CollectorFunc(
			func(context.Context, dataprivacy.Subject) (json.RawMessage, error) {
				return json.RawMessage(`{}`), nil
			},
		)))

		// Supplying the uploader is what satisfies the export worker's storage
		// requirement and wires the signer in one step.
		worker, err := NewWorker(t.Context(), cfg, nil, nil, nil, store, registry, noop.NewUploadManager(), false)
		must.NoError(t, err)
		test.NotNil(t, worker)
	})
}

// configEnv is a SQLite database with a uniquely prefixed request table.
type configEnv struct {
	client database.Client
	prefix string
}

var configPrefixCounter atomic.Uint64

func newConfigEnv(t *testing.T) *configEnv {
	t.Helper()

	client, err := sqlite.NewDatabaseClient(t.Context(),
		&testClientConfig{connectionString: filepath.Join(t.TempDir(), "dataprivacy.db")})
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	prefix := fmt.Sprintf("cfg_%d", configPrefixCounter.Add(1))

	stmts, err := migrations.Statements(dialect.SQLite, prefix)
	must.NoError(t, err)

	for _, stmt := range stmts {
		_, execErr := client.Writer().ExecContext(t.Context(), stmt)
		must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
	}

	return &configEnv{client: client, prefix: prefix}
}

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
