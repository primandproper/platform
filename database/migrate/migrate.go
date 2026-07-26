package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"io/fs"
	"time"

	"github.com/primandproper/platform-go/v7/database"
	"github.com/primandproper/platform-go/v7/errors"
	"github.com/primandproper/platform-go/v7/observability"
	"github.com/primandproper/platform-go/v7/observability/keys"
	"github.com/primandproper/platform-go/v7/observability/logging"
	"github.com/primandproper/platform-go/v7/observability/metrics"
	"github.com/primandproper/platform-go/v7/observability/tracing"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// serviceName names the Migrator's span, logger, and metrics.
const serviceName = "database_migrator"

// Dialect selects the SQL dialect migrations run under.
type Dialect string

const (
	// DialectPostgres runs migrations against PostgreSQL, serialized by a
	// session advisory lock unless WithoutLock is set.
	DialectPostgres Dialect = "postgres"
	// DialectMySQL runs migrations against MySQL.
	DialectMySQL Dialect = "mysql"
	// DialectSQLite runs migrations against SQLite. SQLite is single-writer
	// by nature, so no cross-process lock is taken.
	DialectSQLite Dialect = "sqlite"
)

var _ database.Migrator = (*Migrator)(nil)

// Migrator applies embedded goose SQL migrations. Construct with New; the
// zero value is not usable.
type Migrator struct {
	o11y            observability.Observer
	logger          logging.Logger
	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider
	fsys            fs.FS
	runCounter      metrics.Int64Counter
	appliedCounter  metrics.Int64Counter
	errCounter      metrics.Int64Counter
	latencyHist     metrics.Float64Histogram
	lockKey         string
	dialect         Dialect
	withoutLock     bool
}

// Option configures a Migrator.
type Option func(*Migrator)

// WithLogger attaches a logger. Goose's own progress output is routed through
// it too, so migration logs are structured and attributable instead of going
// to the standard library's global logger.
func WithLogger(logger logging.Logger) Option {
	return func(m *Migrator) {
		m.logger = logger
	}
}

// WithTracerProvider attaches a tracer provider. Migrate is worth tracing: it
// is typically the longest blocking step in service startup, and on Postgres
// it can spend up to a minute waiting on the advisory lock behind a peer that
// is migrating.
func WithTracerProvider(tracerProvider tracing.TracerProvider) Option {
	return func(m *Migrator) {
		m.tracerProvider = tracerProvider
	}
}

// WithMetricsProvider attaches a metrics provider, enabling the
// database_migrator_* instruments.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(m *Migrator) {
		m.metricsProvider = metricsProvider
	}
}

// WithLockKey partitions the Postgres advisory lock ID. Deployments sharing a
// database should share a key (the default empty key is fine); schema-isolated
// parallel tests pass their schema name so they migrate concurrently instead
// of queueing on one global lock.
func WithLockKey(key string) Option {
	return func(m *Migrator) {
		m.lockKey = key
	}
}

// WithoutLock disables the Postgres session advisory lock. Only safe when
// exactly one process can be migrating at a time.
func WithoutLock() Option {
	return func(m *Migrator) {
		m.withoutLock = true
	}
}

// New builds a Migrator over an fs.FS of goose SQL migration files (usually
// an embed.FS subtree; files named like 00001_description.sql containing
// `-- +goose Up` sections).
func New(dialect Dialect, migrations fs.FS, opts ...Option) (*Migrator, error) {
	if migrations == nil {
		return nil, errors.New("nil migrations filesystem provided")
	}
	if _, err := gooseDialect(dialect); err != nil {
		return nil, err
	}

	m := &Migrator{
		dialect: dialect,
		fsys:    migrations,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}

	m.o11y = observability.NewObserver(serviceName, m.logger, m.tracerProvider)
	m.logger = m.o11y.Logger()

	mp := metrics.EnsureMetricsProvider(m.metricsProvider)

	var err error
	if m.runCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_runs", serviceName)); err != nil {
		return nil, errors.Wrap(err, "creating migration run counter")
	}
	if m.appliedCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_applied", serviceName)); err != nil {
		return nil, errors.Wrap(err, "creating migrations applied counter")
	}
	if m.errCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_errors", serviceName)); err != nil {
		return nil, errors.Wrap(err, "creating migration error counter")
	}
	if m.latencyHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", serviceName)); err != nil {
		return nil, errors.Wrap(err, "creating migration latency histogram")
	}

	return m, nil
}

// gooseLogger adapts goose's Printf/Fatalf logger onto the platform logger, so
// goose's progress output joins the service's structured logs rather than the
// standard library's global logger. Fatalf deliberately does not exit: goose
// calls it for conditions it also returns as errors, and a library has no
// business terminating its host process.
type gooseLogger struct {
	logger logging.Logger
}

var _ goose.Logger = (*gooseLogger)(nil)

func (g *gooseLogger) Printf(format string, v ...any) {
	g.logger.Info(fmt.Sprintf(format, v...))
}

func (g *gooseLogger) Fatalf(format string, v ...any) {
	g.logger.Error("migration failure reported by goose", errors.New(fmt.Sprintf(format, v...)))
}

// Migrate implements database.Migrator: it applies all pending migrations,
// and is idempotent — an up-to-date database is a no-op. Concurrent callers
// against one Postgres database serialize on the session advisory lock, so
// racing replicas wait for the winner instead of erroring.
func (m *Migrator) Migrate(ctx context.Context, db *sql.DB) error {
	ctx, op := m.o11y.Begin(ctx)
	defer op.End()
	op.Set("migrate.dialect", string(m.dialect))

	if db == nil {
		return errors.New("nil database provided")
	}

	m.runCounter.Add(ctx, 1)

	startTime := time.Now()
	defer func() {
		m.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	dialect, err := gooseDialect(m.dialect)
	if err != nil {
		m.errCounter.Add(ctx, 1)

		return op.Error(err, "resolving migration dialect")
	}

	providerOpts := []goose.ProviderOption{goose.WithLogger(&gooseLogger{logger: op.Logger()})}

	locked := m.dialect == DialectPostgres && !m.withoutLock
	op.Set("migrate.locked", locked)

	if locked {
		id := lockID(m.lockKey)
		op.Set(keys.LockKeyKey, m.lockKey).Set(keys.LockIDKey, id)

		locker, lockErr := lock.NewPostgresSessionLocker(
			lock.WithLockID(id),
			// Probe every second rather than goose's 5s default, so a waiting
			// replica notices the winner promptly; give up after a minute.
			lock.WithLockTimeout(1, 60),
			lock.WithUnlockTimeout(1, 30),
		)
		if lockErr != nil {
			m.errCounter.Add(ctx, 1)

			return op.Error(lockErr, "building migration session locker")
		}

		providerOpts = append(providerOpts, goose.WithSessionLocker(locker))
	}

	// Instance-based provider: no package-global goose state, so parallel
	// tests (and concurrent Migrators generally) never race on configuration.
	provider, err := goose.NewProvider(dialect, db, m.fsys, providerOpts...)
	if err != nil {
		m.errCounter.Add(ctx, 1)

		return op.Error(err, "building migration provider")
	}

	// Logged before Up rather than after, because Up is where a losing replica
	// blocks on the advisory lock for up to a minute. Without this line, that
	// wait is indistinguishable from a hang.
	if locked {
		op.Logger().Info("acquiring migration lock and applying migrations")
	}

	results, err := provider.Up(ctx)
	if err != nil {
		m.errCounter.Add(ctx, 1)

		return op.Error(err, "applying migrations")
	}

	applied := make([]string, 0, len(results))
	for _, result := range results {
		applied = append(applied, result.Source.Path)
	}

	m.appliedCounter.Add(ctx, int64(len(results)))
	op.SetValues(map[string]any{"migrate.applied": len(results), "migrate.versions": applied}).
		Logger().
		WithValue("duration_ms", time.Since(startTime).Milliseconds()).
		Info("migrations applied")

	return nil
}

// gooseDialect maps the package's Dialect to goose's, rejecting unknowns.
func gooseDialect(d Dialect) (goose.Dialect, error) {
	switch d {
	case DialectPostgres:
		return goose.DialectPostgres, nil
	case DialectMySQL:
		return goose.DialectMySQL, nil
	case DialectSQLite:
		return goose.DialectSQLite3, nil
	default:
		return "", errors.Newf("unknown migration dialect %q", d)
	}
}

// lockID derives a stable advisory-lock ID from the lock key using FNV-64a. A
// hash collision between two keys merely over-serializes their migrations —
// never corruption.
func lockID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("platform-migrations:" + key))

	return int64(h.Sum64())
}
