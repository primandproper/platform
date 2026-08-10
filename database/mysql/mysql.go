package mysql

import (
	"context"
	"database/sql"
	"time"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	"github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/XSAM/otelsql"
	_ "github.com/go-sql-driver/mysql"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

const (
	tracingName = "db_client"
)

// Client is the primary database querying client.
type Client struct {
	o11y     observability.Observer
	timeFunc func() time.Time
	config   database.ClientConfig
	readDB   *sql.DB
	writeDB  *sql.DB
}

var (
	_ database.Client    = (*Client)(nil)
	_ database.RawAccess = (*Client)(nil)
)

// NewDatabaseClient provides a new DataManager client.
// If a metrics provider is supplied via WithMetricsProvider, the DB driver will
// use it so SQL latency and other db.sql.* metrics are emitted (e.g.
// db_sql_latency_milliseconds_bucket in Prometheus).
func NewDatabaseClient(ctx context.Context, cfg database.ClientConfig, opts ...Option) (*Client, error) {
	o := newOptions(opts)
	o11y := observability.NewObserver(tracingName, o.logger, o.tracerProvider)

	_, op := o11y.Begin(ctx)
	defer op.End()

	otelsqlOpts := []otelsql.Option{
		otelsql.WithAttributes(
			attribute.KeyValue{
				Key:   semconv.ServiceNameKey,
				Value: attribute.StringValue("database"),
			},
		),
	}
	if o.metricsProvider != nil {
		otelsqlOpts = append(otelsqlOpts, otelsql.WithMeterProvider(o.metricsProvider.MeterProvider()))
	}

	// Gate raw SQL text on spans behind the config's LogQueries flag. When the
	// config opts out (the default), suppress db.statement so query text is not
	// leaked into traces.
	if lq, ok := cfg.(interface{ GetLogQueries() bool }); ok && !lq.GetLogQueries() {
		otelsqlOpts = append(otelsqlOpts, otelsql.WithSpanOptions(otelsql.SpanOptions{DisableQuery: true}))
	}

	var readDB, writeDB *sql.DB
	var err error

	readConnStr := cfg.GetReadConnectionString()
	writeConnStr := cfg.GetWriteConnectionString()

	op.Set("db.system", "mysql").
		Set("db.read_configured", readConnStr != "").
		Set("db.write_configured", writeConnStr != "")

	if readConnStr != "" {
		readDB, err = connect(readConnStr, cfg, otelsqlOpts)
		if err != nil {
			return nil, errors.Wrap(err, "connecting to read mysql database")
		}
	}

	if writeConnStr != "" {
		writeDB, err = connect(writeConnStr, cfg, otelsqlOpts)
		if err != nil {
			// Don't leak the read side when the write side fails to construct —
			// the same fix postgres already carries.
			return nil, closePools(errors.Wrap(err, "connecting to write mysql database"), readDB, nil)
		}
	}

	// Fall back: if only one connection is configured, use it for both.
	if readDB == nil && writeDB == nil {
		return nil, errors.New("at least one of read or write connection string must be provided")
	}
	if readDB == nil {
		readDB = writeDB
	}
	if writeDB == nil {
		writeDB = readDB
	}

	if o.metricsProvider != nil {
		// Both pools are open by this point, so every failure path below has to
		// close them; returning early here leaked a fully-connected pool pair.
		if _, err = otelsql.RegisterDBStatsMetrics(readDB, otelsql.WithAttributes(semconv.DBSystemMySQL)); err != nil {
			return nil, closePools(errors.Wrap(err, "registering readDB stats metrics"), readDB, writeDB)
		}

		if readDB != writeDB {
			if _, err = otelsql.RegisterDBStatsMetrics(writeDB, otelsql.WithAttributes(semconv.DBSystemMySQL)); err != nil {
				return nil, closePools(errors.Wrap(err, "registering writeDB stats metrics"), readDB, writeDB)
			}
		}
	}

	c := &Client{
		readDB:   readDB,
		writeDB:  writeDB,
		config:   cfg,
		o11y:     o11y,
		timeFunc: defaultTimeFunc,
	}

	return c, nil
}

// closePools releases whatever was opened, for the failure paths after a
// successful connect. Read and write may be the same handle when only one
// connection string is configured, so it is closed once.
func closePools(cause error, readDB, writeDB *sql.DB) error {
	if readDB != nil {
		if closeErr := readDB.Close(); closeErr != nil {
			cause = errors.Join(cause, errors.Wrap(closeErr, "closing read database"))
		}
	}

	if writeDB != nil && writeDB != readDB {
		if closeErr := writeDB.Close(); closeErr != nil {
			cause = errors.Join(cause, errors.Wrap(closeErr, "closing write database"))
		}
	}

	return cause
}

func connect(connStr string, cfg database.ClientConfig, opts []otelsql.Option) (*sql.DB, error) {
	db, err := otelsql.Open("mysql", connStr, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "connecting to mysql database")
	}

	db.SetMaxIdleConns(cfg.GetMaxIdleConns())
	db.SetMaxOpenConns(cfg.GetMaxOpenConns())
	db.SetConnMaxLifetime(cfg.GetConnMaxLifetime())

	return db, nil
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

// Dialect reports the SQL dialect this client speaks, which is always
// dialect.MySQL.
func (*Client) Dialect() dialect.Dialect {
	return dialect.MySQL
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

// Close closes the database connection.
func (q *Client) Close() error {
	logger := q.o11y.Logger()

	var errs error

	if err := q.readDB.Close(); err != nil {
		logger.Error("closing read database connection", err)
		errs = errors.Join(errs, err)
	}

	// Always attempt to close the write pool even if the read pool failed to close,
	// so a read-close error can't leak the write connection.
	if q.writeDB != q.readDB {
		if err := q.writeDB.Close(); err != nil {
			logger.Error("closing write database connection", err)
			errs = errors.Join(errs, err)
		}
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
