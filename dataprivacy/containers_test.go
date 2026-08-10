package dataprivacy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v10/database"
	"github.com/primandproper/platform-go/v10/database/dialect"
	"github.com/primandproper/platform-go/v10/database/mysql"
	"github.com/primandproper/platform-go/v10/database/postgres"
	"github.com/primandproper/platform-go/v10/dataprivacy/migrations"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/operations"
	opsmigrations "github.com/primandproper/platform-go/v10/operations/migrations"
	"github.com/primandproper/platform-go/v10/testutils/containers/mysqltest"
	"github.com/primandproper/platform-go/v10/testutils/containers/pgtest"
	"github.com/primandproper/platform-go/v10/workqueue"
	workqueuemigrations "github.com/primandproper/platform-go/v10/workqueue/migrations"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// defaultMySQLImage pins the MariaDB flavor this suite exercises; mysqltest's
// default is stock MySQL.
const defaultMySQLImage = "mariadb:11"

// pooledClientConfig is testClientConfig with a pool big enough to fulfill a
// request.
//
// One connection is enough for the store suite, which issues one statement at a
// time, and is not enough for a runner: a fulfillment holds a transaction open
// while the operation's progress flush writes to the operations table on the
// same pool, so a pool of one deadlocks. That is a real constraint on deploying
// this rather than an artifact of the test — see Fulfiller.erase.
type pooledClientConfig struct {
	testClientConfig
}

func (*pooledClientConfig) GetMaxIdleConns() int { return 8 }

func (*pooledClientConfig) GetMaxOpenConns() int { return 16 }

// TestSQLStore_RealServers runs the same behavioral suite SQLite runs, against
// real servers.
//
// It exists because the SQL that only a real server can validate is otherwise
// merely rendered, never executed: numbered placeholders, SKIP LOCKED, MySQL's
// derived-table rewrite for the UPDATE and DELETE that read their own table,
// the partial indexes Postgres and SQLite have and MySQL does not, and native
// timestamp and boolean handling across three drivers.
//
// The lapse and reap queries are the ones this catches. Both wrap a subquery
// over the table they are modifying, which MySQL rejects outright without the
// derived table — and SQLite accepts either way, so it can never tell us.
func TestSQLStore_RealServers(T *testing.T) {
	T.Parallel()

	T.Run("postgres", func(t *testing.T) {
		t.Parallel()

		pgtest.Run(t, func(_ context.Context, pg *pgtest.Instance) {
			client, err := postgres.NewDatabaseClient(t.Context(),
				&testClientConfig{connectionString: pg.ConnectionString})
			must.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })

			runStoreSuite(t, &storeEnv{client: client, dialect: dialect.Postgres})
		})
	})

	T.Run("mysql", func(t *testing.T) {
		t.Parallel()

		runWithMySQL(t, func(_ context.Context, client database.Client) {
			runStoreSuite(t, &storeEnv{client: client, dialect: dialect.MySQL})
		})
	})
}

// TestMigrations_RealServers proves the shipped DDL is accepted verbatim by
// each server, independent of whether the store then exercises every column.
func TestMigrations_RealServers(T *testing.T) {
	T.Parallel()

	T.Run("postgres", func(t *testing.T) {
		t.Parallel()

		pgtest.Run(t, func(ctx context.Context, pg *pgtest.Instance) {
			stmts, err := migrations.Statements(dialect.Postgres, "ddl_check")
			must.NoError(t, err)

			// Executed twice: every statement is IF NOT EXISTS, so re-running a
			// migration must be a no-op rather than an error.
			for range 2 {
				for _, stmt := range stmts {
					_, execErr := pg.DB.ExecContext(ctx, stmt)
					must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
				}
			}
		})
	})

	T.Run("mysql", func(t *testing.T) {
		t.Parallel()

		runWithMySQL(t, func(ctx context.Context, client database.Client) {
			stmts, err := migrations.Statements(dialect.MySQL, "ddl_check")
			must.NoError(t, err)

			// MySQL has no CREATE INDEX IF NOT EXISTS, so unlike Postgres this
			// runs once — the table carries IF NOT EXISTS, the indexes cannot.
			for _, stmt := range stmts {
				_, execErr := client.Writer().ExecContext(ctx, stmt)
				must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
			}
		})
	})

	T.Run("statements carry no unrendered placeholder", func(t *testing.T) {
		t.Parallel()

		for _, d := range []dialect.Dialect{dialect.Postgres, dialect.MySQL, dialect.SQLite} {
			stmts, err := migrations.Statements(d, "ddl_check")
			must.NoError(t, err)

			for _, stmt := range stmts {
				test.False(t, strings.Contains(stmt, "{{"))
			}
		}
	})
}

// TestFulfillment_Postgres runs one export and one erasure the whole way
// through, against a real operations worker.
//
// It is the only test that can say the port worked. Everything above drives a
// runner directly with a reporter the test owns, which proves the runner does
// the right thing and proves nothing about whether an operation started by
// Submit is the one a worker claims, whether the kinds registered are the kinds
// the row names, whether the progress a client would read is the progress the
// runner reported, or whether the artifact is fetchable at the end of it.
//
// Postgres only, because operations is: the guarded claim is one
// UPDATE … RETURNING, and the queue underneath is Postgres-only for its own
// reasons.
func TestFulfillment_Postgres(T *testing.T) {
	T.Parallel()

	pgtest.Run(T, func(ctx context.Context, pg *pgtest.Instance) {
		client, clientErr := postgres.NewDatabaseClient(ctx,
			&pooledClientConfig{testClientConfig{connectionString: pg.ConnectionString}})
		must.NoError(T, clientErr)
		T.Cleanup(func() { _ = client.Close() })

		T.Run("an export runs from submit to a fetchable artifact", func(t *testing.T) {
			t.Parallel()

			env := newFulfillmentEnv(t, client, func(r *Registry) {
				must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"email":"a@example.com"}`)))
				must.NoError(t, r.RegisterCollector("billing", staticCollector(`{"invoices":2}`)))
				must.NoError(t, r.RegisterCollector("webhooks", staticCollector(`{"hooks":[]}`)))
			})

			req, err := env.svc.Submit(t.Context(), testSubject, RequestExport)
			must.NoError(t, err)
			must.StrNotEqFold(t, "", req.OperationID)

			op := env.drain(t, req.OperationID)

			test.EqOp(t, operations.StateSucceeded, op.State)
			test.True(t, op.Done)

			// The operation's owner is the subject, which is what lets
			// operations/http scope a status read to the person it is about.
			test.EqOp(t, testSubject.ID, op.Owner)
			test.EqOp(t, KindExport, op.Kind)

			// The domains are the unit denominator, for free: the registry
			// already enumerates them, so "3 of 3" needs no counting pass.
			must.NotNil(t, op.Progress.UnitsTotal)
			test.EqOp(t, 3, *op.Progress.UnitsTotal)
			test.EqOp(t, 3, op.Progress.UnitsDone)
			test.EqOp(t, "bytes", op.Progress.CountLabel)
			test.Greater(t, int64(0), op.Progress.Count)

			fraction, ok := op.Progress.Fraction()
			must.True(t, ok)
			test.EqOp(t, float64(1), fraction)

			// The artifact is the result pointer, and the summary beside it is
			// the manifest minus the subject.
			must.NotNil(t, op.Result)
			must.StrNotEqFold(t, "", op.Result.URI)

			var summary ExportSummary
			must.NoError(t, json.Unmarshal(op.Result.Detail, &summary))
			test.MapEmpty(t, summary.Failures)
			test.Greater(t, int64(0), summary.Bytes)

			// And the request row is the statutory record, pointing at both.
			read, err := env.svc.Get(t.Context(), req.ID)
			must.NoError(t, err)
			test.EqOp(t, StatusCompleted, read.Status)
			test.EqOp(t, op.ID, read.OperationID)
			test.EqOp(t, op.Result.URI, read.ArtifactRef)
			test.False(t, read.ExpiresAt.IsZero())

			// Delivered, and readable: Open reverses whatever packaging the
			// fulfiller applied, which is the path that works everywhere.
			artifact, err := env.svc.Open(t.Context(), req.ID)
			must.NoError(t, err)

			t.Cleanup(func() { _ = artifact.Close() })

			body, err := io.ReadAll(artifact)
			must.NoError(t, err)

			var doc Document
			must.NoError(t, json.Unmarshal(body, &doc))

			test.EqOp(t, DocumentFormat, doc.Manifest.Format)
			test.EqOp(t, req.ID, doc.Manifest.RequestID)
			test.Eq(t, []string{"billing", "identity", "webhooks"}, doc.Manifest.Sections)
		})

		T.Run("a partial export is delivered and says what is missing", func(t *testing.T) {
			t.Parallel()

			env := newFulfillmentEnv(t, client, func(r *Registry) {
				must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
				must.NoError(t, r.RegisterCollector("billing", failingCollector(platformerrors.New("billing is down"))))
			})

			req, err := env.svc.Submit(t.Context(), testSubject, RequestExport)
			must.NoError(t, err)

			op := env.drain(t, req.OperationID)

			// A successful operation with the gap recorded, not a failure. The
			// export exists and the subject is entitled to it; which sections
			// are missing is a fact about the answer rather than about whether
			// there is one.
			test.EqOp(t, operations.StateSucceeded, op.State)

			var summary ExportSummary
			must.NoError(t, json.Unmarshal(op.Result.Detail, &summary))
			must.MapLen(t, 1, summary.Failures)
			test.StrContains(t, summary.Failures["billing"], "billing is down")

			read, err := env.svc.Get(t.Context(), req.ID)
			must.NoError(t, err)
			test.EqOp(t, StatusCompleted, read.Status)
			test.True(t, read.Partial())
		})

		T.Run("an erasure waits for confirmation, then runs", func(t *testing.T) {
			t.Parallel()

			var erased atomic.Int64

			env := newFulfillmentEnv(t, client, func(r *Registry) {
				must.NoError(t, r.RegisterCollector("identity", staticCollector(`{"ok":true}`)))
				must.NoError(t, r.RegisterEraser("identity", countingEraser(5, 1, nil, &erased)))
				must.NoError(t, r.RegisterEraser("billing",
					countingEraser(2, 0, map[string]string{"invoices": "tax law"}, nil)))
			}, WithFulfillerConfirmationWindow(72*time.Hour))

			req, err := env.svc.Submit(t.Context(), testSubject, RequestErasure)
			must.NoError(t, err)

			// Nothing runs and nothing is queued: until somebody confirms it,
			// there is no operation at all.
			test.EqOp(t, StatusAwaitingConfirmation, req.Status)
			test.EqOp(t, "", req.OperationID)

			confirmed, err := env.svc.Confirm(t.Context(), req.ID)
			must.NoError(t, err)
			must.StrNotEqFold(t, "", confirmed.OperationID)

			op := env.drain(t, confirmed.OperationID)

			test.EqOp(t, operations.StateSucceeded, op.State)
			test.EqOp(t, KindErasure, op.Kind)
			test.EqOp(t, int64(1), erased.Load())

			// The erasers are the unit tier and the rows are the count.
			must.NotNil(t, op.Progress.UnitsTotal)
			test.EqOp(t, 2, *op.Progress.UnitsTotal)
			test.EqOp(t, 2, op.Progress.UnitsDone)
			test.EqOp(t, int64(8), op.Progress.Count)

			var summary ErasureSummary
			must.NoError(t, json.Unmarshal(op.Result.Detail, &summary))
			test.EqOp(t, int64(7), summary.Deleted)
			test.EqOp(t, int64(1), summary.Anonymized)
			test.EqOp(t, "tax law", summary.Retained["billing.invoices"])

			read, err := env.svc.Get(t.Context(), req.ID)
			must.NoError(t, err)
			test.EqOp(t, StatusCompleted, read.Status)
			test.EqOp(t, int64(7), read.Deleted)

			// An erasure has no artifact, so nothing expires.
			test.EqOp(t, "", read.ArtifactRef)
			test.True(t, read.ExpiresAt.IsZero())
		})

		T.Run("a request that cannot be fulfilled ends failed on both records", func(t *testing.T) {
			t.Parallel()

			env := newFulfillmentEnv(t, client, func(r *Registry) {
				must.NoError(t, r.RegisterCollector("identity", failingCollector(platformerrors.New("down"))))
			})

			req, err := env.svc.Submit(t.Context(), testSubject, RequestExport)
			must.NoError(t, err)

			op := env.drain(t, req.OperationID)

			test.EqOp(t, operations.StateFailed, op.State)
			must.NotNil(t, op.Error)
			test.EqOp(t, operations.CodeAttemptsExhausted, op.Error.Code)

			// The row is marked on the final attempt and not before, which is
			// the only moment at which "nobody is getting an answer" is true.
			read, err := env.svc.Get(t.Context(), req.ID)
			must.NoError(t, err)
			test.EqOp(t, StatusFailed, read.Status)
			test.StrContains(t, read.LastError, "no dataprivacy collector succeeded")
			must.NotNil(t, read.CompletedAt)

			// And it drops off the overdue gauge, because it is terminal: the
			// request is not still owed in the sense that gauge measures.
			test.False(t, read.Overdue(read.DueAt.Add(time.Hour)))
		})
	})
}

// fulfillmentEnv is the whole stack over one Postgres database: a dataprivacy
// store and service, an operations store, queue, service, and worker, and a
// fulfiller registered into the registry the worker runs from.
type fulfillmentEnv struct {
	svc    Service
	worker *operations.Worker
	ops    operations.Service
}

// fulfillmentCounter names a fresh table namespace and queue per subtest.
// Subtests share one database and must not share a queue: one test's backlog
// would be another's, and a worker claiming somebody else's operation would run
// a kind its registry does not have.
var fulfillmentCounter atomic.Uint64

// fulfillmentOption customizes what newFulfillmentEnv assembles. There are two,
// and neither is a dataprivacy option — they configure the assembly rather than
// any one part of it.
type fulfillmentOption func(*ServiceConfig)

// WithFulfillerConfirmationWindow holds erasures for confirmation.
func WithFulfillerConfirmationWindow(window time.Duration) fulfillmentOption {
	return func(cfg *ServiceConfig) { cfg.ConfirmationWindow = window }
}

func newFulfillmentEnv(
	t *testing.T,
	client database.Client,
	register func(*Registry),
	opts ...fulfillmentOption,
) *fulfillmentEnv {
	t.Helper()

	prefix := fmt.Sprintf("dpe%d", fulfillmentCounter.Add(1))

	// Three schemas, one namespace: the request table, the operations table, and
	// the work queue the operations are dispatched through.
	for _, render := range []func(dialect.Dialect, string) ([]string, error){
		migrations.Statements,
		opsmigrations.Statements,
		workqueuemigrations.Statements,
	} {
		statements, err := render(dialect.Postgres, prefix)
		must.NoError(t, err)
		must.SliceNotEmpty(t, statements)

		for _, stmt := range statements {
			_, execErr := client.Writer().ExecContext(t.Context(), stmt)
			must.NoError(t, execErr, must.Sprintf("executing %q", stmt))
		}
	}

	domains := NewRegistry()
	register(domains)

	store, err := NewSQLStore(client, WithTablePrefix(prefix))
	must.NoError(t, err)

	uploader := newMemoryUploader()

	fulfiller, err := NewFulfiller(t.Context(), &FulfillerConfig{}, store, domains,
		WithFulfillerUploadManager(uploader))
	must.NoError(t, err)

	kinds := operations.NewRegistry()
	must.NoError(t, fulfiller.Register(kinds))

	opsStore, err := operations.NewSQLStore(client, operations.WithStoreTablePrefix(prefix))
	must.NoError(t, err)

	queue, err := workqueue.New[string](t.Context(),
		&workqueue.Config{Name: prefix, TablePrefix: prefix}, client)
	must.NoError(t, err)

	t.Cleanup(func() { _ = queue.Close(context.WithoutCancel(t.Context())) })

	opsCfg := &operations.Config{QueueName: prefix, TablePrefix: prefix}

	opsSvc, err := operations.NewService(t.Context(), opsCfg, opsStore, queue, kinds)
	must.NoError(t, err)

	worker, err := operations.NewWorker(t.Context(), &operations.WorkerConfig{
		Lease:            10 * time.Second,
		ProgressInterval: 100 * time.Millisecond,
		Batch:            4,
		Concurrency:      2,
		// Two rather than the package default, so the failure subtest exhausts
		// a budget in a couple of passes rather than in five.
		MaxAttempts: 2,
		RetryDelay:  10 * time.Millisecond,
	}, opsStore, queue, kinds)
	must.NoError(t, err)

	cfg := &ServiceConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	svc, err := NewService(t.Context(), cfg, store, opsSvc, WithServiceUploadManager(uploader))
	must.NoError(t, err)

	// The real loop, stopped by cancelling its context — which is how an
	// operations worker is always stopped.
	workerCtx, stop := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(stop)

	go func() { _ = worker.Run(workerCtx) }()

	return &fulfillmentEnv{svc: svc, worker: worker, ops: opsSvc}
}

// drain polls the operation until it reaches a terminal state.
//
// The worker is a real one, running its real loop — see newFulfillmentEnv —
// because the claim, the lease, the progress flush, and the retry are exactly
// what this file exists to exercise. Only the waiting is the test's.
func (e *fulfillmentEnv) drain(t *testing.T, operationID string) *operations.Operation {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		op, err := e.ops.Get(t.Context(), operationID)
		must.NoError(t, err)

		if op.Terminal() {
			return op
		}

		time.Sleep(20 * time.Millisecond)
	}

	op, err := e.ops.Get(t.Context(), operationID)
	must.NoError(t, err)

	t.Fatalf("operation %q never reached a terminal state: %+v", operationID, op)

	return nil
}

// runWithMySQL starts a MySQL-flavored container and hands the closure a
// database.Client against it.
func runWithMySQL(t *testing.T, fn func(ctx context.Context, client database.Client)) {
	t.Helper()

	mysqltest.Run(t, func(ctx context.Context, my *mysqltest.Instance) {
		client, err := mysql.NewDatabaseClient(ctx,
			&testClientConfig{connectionString: my.ConnectionString})
		must.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })

		fn(ctx, client)
	}, mysqltest.WithImage(defaultMySQLImage))
}
