package migrate

import (
	"context"
	"database/sql"
	"hash/fnv"
	"io/fs"

	"github.com/primandproper/platform-go/v7/database"
	"github.com/primandproper/platform-go/v7/errors"
	"github.com/primandproper/platform-go/v7/observability/logging"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

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
	logger      logging.Logger
	fsys        fs.FS
	lockKey     string
	dialect     Dialect
	withoutLock bool
}

// Option configures a Migrator.
type Option func(*Migrator)

// WithLogger attaches a logger; migrations log the number applied.
func WithLogger(logger logging.Logger) Option {
	return func(m *Migrator) {
		m.logger = logging.EnsureLogger(logger)
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
		logger:  logging.EnsureLogger(nil),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}

	return m, nil
}

// Migrate implements database.Migrator: it applies all pending migrations,
// and is idempotent — an up-to-date database is a no-op. Concurrent callers
// against one Postgres database serialize on the session advisory lock, so
// racing replicas wait for the winner instead of erroring.
func (m *Migrator) Migrate(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("nil database provided")
	}

	dialect, err := gooseDialect(m.dialect)
	if err != nil {
		return err
	}

	var providerOpts []goose.ProviderOption

	if m.dialect == DialectPostgres && !m.withoutLock {
		locker, lockErr := lock.NewPostgresSessionLocker(
			lock.WithLockID(lockID(m.lockKey)),
			// Probe every second rather than goose's 5s default, so a waiting
			// replica notices the winner promptly; give up after a minute.
			lock.WithLockTimeout(1, 60),
			lock.WithUnlockTimeout(1, 30),
		)
		if lockErr != nil {
			return errors.Wrap(lockErr, "building migration session locker")
		}

		providerOpts = append(providerOpts, goose.WithSessionLocker(locker))
	}

	// Instance-based provider: no package-global goose state, so parallel
	// tests (and concurrent Migrators generally) never race on configuration.
	provider, err := goose.NewProvider(dialect, db, m.fsys, providerOpts...)
	if err != nil {
		return errors.Wrap(err, "building migration provider")
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return errors.Wrap(err, "applying migrations")
	}

	m.logger.WithValue("applied", len(results)).Info("migrations applied")

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
