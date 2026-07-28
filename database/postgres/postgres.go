package postgres

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/primandproper/platform-go/v8/database"
	"github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"

	"github.com/XSAM/otelsql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	tracingName = "db_client"
)

// PgxAccess is an optional capability exposing the native pgx connection pools, for
// callers that need driver features the database/sql surface cannot express —
// CopyFrom bulk loads, pgx.Batch, native array binding, or LISTEN/NOTIFY. Obtain it
// by asserting on a Client:
//
//	native, ok := client.(postgres.PgxAccess)
//
// The returned pools are the very pools backing Reader, Writer, and RawAccess — the
// database/sql handles are derived from them via a pool connector — so
// MaxOpenConns caps the union of both surfaces, and a connection held idle by the
// database/sql layer is unavailable to native callers until it is released.
//
// Like RawAccess, this is a deliberate step outside the portable Client surface; it
// is also postgres-only, so callers asserting it accept a hard pgx dependency.
type PgxAccess interface {
	ReadPool() *pgxpool.Pool
	WritePool() *pgxpool.Pool
}

// Client is the primary database querying client.
type Client struct {
	o11y      observability.Observer
	timeFunc  func() time.Time
	config    database.ClientConfig
	readPool  *pgxpool.Pool
	writePool *pgxpool.Pool
	readDB    *sql.DB
	writeDB   *sql.DB
}

var (
	_ database.Client    = (*Client)(nil)
	_ database.RawAccess = (*Client)(nil)
	_ PgxAccess          = (*Client)(nil)
)

// NewDatabaseClient provides a new DataManager client.
//
// Construction is pgx-native-first: each side opens a *pgxpool.Pool (reachable via
// the PgxAccess capability) and derives its database/sql handle from that pool, so
// both surfaces share one set of connections. The database/sql layer keeps its
// otelsql instrumentation; if metricsProvider is non-nil, the driver emits SQL
// latency and other db.sql.* metrics (e.g. db_sql_latency_milliseconds_bucket in
// Prometheus). Native pool usage is not yet traced — instrument at the call site,
// or thread a pgx tracer through here when a consumer needs it.
func NewDatabaseClient(ctx context.Context, logger logging.Logger, tracerProvider tracing.TracerProvider, cfg database.ClientConfig, metricsProvider metrics.Provider) (database.Client, error) {
	o11y := observability.NewObserver(tracingName, logger, tracerProvider)

	ctx, op := o11y.Begin(ctx)
	defer op.End()

	opts := []otelsql.Option{
		otelsql.WithAttributes(
			attribute.KeyValue{
				Key:   semconv.ServiceNameKey,
				Value: attribute.StringValue("database"),
			},
		),
	}
	if metricsProvider != nil {
		opts = append(opts, otelsql.WithMeterProvider(metricsProvider.MeterProvider()))
	}

	// Gate raw SQL text on spans behind the config's LogQueries flag. When the
	// config opts out (the default), suppress db.statement so query text is not
	// leaked into traces.
	if lq, ok := cfg.(interface{ GetLogQueries() bool }); ok && !lq.GetLogQueries() {
		opts = append(opts, otelsql.WithSpanOptions(otelsql.SpanOptions{DisableQuery: true}))
	}

	var (
		readPool, writePool *pgxpool.Pool
		readDB, writeDB     *sql.DB
		err                 error
	)

	readConnStr := cfg.GetReadConnectionString()
	writeConnStr := cfg.GetWriteConnectionString()

	op.Set("db.system", "postgresql").
		Set("db.read_configured", readConnStr != "").
		Set("db.write_configured", writeConnStr != "")

	if readConnStr != "" {
		readPool, readDB, err = connect(ctx, readConnStr, cfg, opts)
		if err != nil {
			return nil, errors.Wrap(err, "connecting to read postgres database")
		}
	}

	if writeConnStr != "" {
		writePool, writeDB, err = connect(ctx, writeConnStr, cfg, opts)
		if err != nil {
			err = errors.Wrap(err, "connecting to write postgres database")

			// Don't leak the read side when the write side fails to construct.
			if readDB != nil {
				if closeErr := readDB.Close(); closeErr != nil {
					err = errors.Join(err, errors.Wrap(closeErr, "closing read database after write connect failure"))
				}
			}
			if readPool != nil {
				readPool.Close()
			}

			return nil, err
		}
	}

	// Fall back: if only one connection is configured, use it for both.
	if readDB == nil && writeDB == nil {
		return nil, errors.New("at least one of read or write connection string must be provided")
	}
	if readDB == nil {
		readPool, readDB = writePool, writeDB
	}
	if writeDB == nil {
		writePool, writeDB = readPool, readDB
	}

	if metricsProvider != nil {
		if _, err = otelsql.RegisterDBStatsMetrics(readDB, otelsql.WithAttributes(semconv.DBSystemPostgreSQL)); err != nil {
			return nil, errors.Wrap(err, "registering readDB stats metrics")
		}

		if readDB != writeDB {
			if _, err = otelsql.RegisterDBStatsMetrics(writeDB, otelsql.WithAttributes(semconv.DBSystemPostgreSQL)); err != nil {
				return nil, errors.Wrap(err, "registering writeDB stats metrics")
			}
		}
	}

	c := &Client{
		readPool:  readPool,
		writePool: writePool,
		readDB:    readDB,
		writeDB:   writeDB,
		config:    cfg,
		o11y:      o11y,
		timeFunc:  defaultTimeFunc,
	}

	return c, nil
}

// connect opens the pgx pool for one side of the read/write split and derives its
// database/sql handle from it. The pool is the single authority on connection
// count and lifetime; the derived handle is capped to the same values so the two
// layers can never disagree about the real limit.
func connect(ctx context.Context, connStr string, cfg database.ClientConfig, opts []otelsql.Option) (*pgxpool.Pool, *sql.DB, error) {
	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parsing postgres connection string")
	}

	// Zero config values keep pgxpool's parsed defaults (max(4, NumCPU) conns,
	// 1h lifetime) rather than being forwarded, mirroring database/sql's
	// "zero means unlimited" without giving pgxpool a nonsensical bound.
	if n := cfg.GetMaxOpenConns(); n > 0 {
		poolCfg.MaxConns = clampToInt32(n)
	}
	if d := cfg.GetConnMaxLifetime(); d > 0 {
		poolCfg.MaxConnLifetime = d
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, errors.Wrap(err, "creating postgres connection pool")
	}

	db := otelsql.OpenDB(stdlib.GetPoolConnector(pool), opts...)

	// An idle connection at this layer is still checked out of the pgx pool, so
	// MaxIdleConns bounds how many pool connections the database/sql surface may
	// pin while unused.
	db.SetMaxIdleConns(cfg.GetMaxIdleConns())
	db.SetMaxOpenConns(cfg.GetMaxOpenConns())
	db.SetConnMaxLifetime(cfg.GetConnMaxLifetime())

	return pool, db, nil
}

// clampToInt32 converts a positive int to int32, saturating at MaxInt32.
func clampToInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}

	return int32(n)
}

// ReadDB provides the database object.
func (q *Client) ReadDB() *sql.DB {
	return q.readDB
}

// WriteDB provides the database object. It satisfies database.RawAccess; prefer Writer
// and WithTransaction on the Client interface.
func (q *Client) WriteDB() *sql.DB {
	return q.writeDB
}

// ReadPool provides the native pgx pool behind the read database. It satisfies
// PgxAccess; see that interface's documentation for the sharing semantics.
func (q *Client) ReadPool() *pgxpool.Pool {
	return q.readPool
}

// WritePool provides the native pgx pool behind the write database. It satisfies
// PgxAccess; see that interface's documentation for the sharing semantics.
func (q *Client) WritePool() *pgxpool.Pool {
	return q.writePool
}

// Reader returns a non-transactional executor for the read database.
func (q *Client) Reader() database.SQLQueryExecutor {
	return q.readDB
}

// Writer returns a non-transactional executor for the write database.
func (q *Client) Writer() database.SQLQueryExecutor {
	return q.writeDB
}

// WithTransaction runs fn inside a transaction on the write database, committing on a
// nil return and rolling back on error or panic. See database.RunInTransaction.
func (q *Client) WithTransaction(ctx context.Context, fn func(tx database.SQLQueryExecutor) error) error {
	ctx, op := q.o11y.Begin(ctx)
	defer op.End()

	return database.RunInTransaction(ctx, q.writeDB, q.RollbackTransaction, fn)
}

// Close closes the database/sql layer first so its connections drain back to the
// pools, then closes the pools themselves. pgxpool's Close blocks until every
// connection is returned, so a connection leaked by a caller (an unclosed Rows,
// an unreleased native Acquire) will hang Close rather than be abandoned.
func (q *Client) Close() error {
	var errs error

	if err := q.readDB.Close(); err != nil {
		q.o11y.Logger().Error("closing read database connection", err)
		errs = errors.Join(errs, err)
	}

	// Always attempt to close the write pool even if the read pool failed to close,
	// so a read-close error can't leak the write connection.
	if q.writeDB != q.readDB {
		if err := q.writeDB.Close(); err != nil {
			q.o11y.Logger().Error("closing write database connection", err)
			errs = errors.Join(errs, err)
		}
	}

	// Pools are nil on clients constructed directly around a plain *sql.DB (tests);
	// the derived handles above are the only layer in that case.
	if q.readPool != nil {
		q.readPool.Close()
	}
	if q.writePool != nil && q.writePool != q.readPool {
		q.writePool.Close()
	}

	return errs
}

// IsReady returns whether the database is ready for the querier.
func (q *Client) IsReady(ctx context.Context) bool {
	ctx, op := q.o11y.Begin(ctx)
	defer op.End()

	maxAttempts := int(q.config.GetMaxPingAttempts())
	waitPeriod := q.config.GetPingWaitPeriod()

	op.Set("db.ping.max_attempts", maxAttempts).Set("db.ping.wait_period", waitPeriod)

	readReady := q.waitForPing(ctx, op, q.readDB, "read", maxAttempts, waitPeriod)
	if !readReady {
		return false
	}

	if q.writeDB != q.readDB {
		return q.waitForPing(ctx, op, q.writeDB, "write", maxAttempts, waitPeriod)
	}

	return true
}

func (q *Client) waitForPing(ctx context.Context, op observability.Operation, db *sql.DB, connectionName string, maxAttempts int, waitPeriod time.Duration) bool {
	logger := op.Logger().WithValue("connection", connectionName)

	for attemptCount := range maxAttempts {
		if err := db.PingContext(ctx); err == nil {
			return true
		}

		logger.WithValue("attempt_count", attemptCount).Info("ping failed, waiting for db")

		// Don't sleep after the final attempt, and abort promptly if the caller's
		// context is canceled rather than sleeping through it.
		if attemptCount == maxAttempts-1 {
			break
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(waitPeriod):
		}
	}

	return false
}

func defaultTimeFunc() time.Time {
	return time.Now()
}

func (q *Client) CurrentTime() time.Time {
	if q == nil || q.timeFunc == nil {
		return defaultTimeFunc()
	}

	return q.timeFunc()
}

func (q *Client) RollbackTransaction(ctx context.Context, tx database.SQLQueryExecutorAndTransactionManager) {
	_, op := q.o11y.Begin(ctx)
	defer op.End()

	op.Logger().Debug("rolling back transaction")

	if err := tx.Rollback(); err != nil {
		op.Acknowledge(err, "rolling back transaction")
	}

	op.Logger().Debug("transaction rolled back")
}
