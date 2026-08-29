package postgres

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v13/database"
	"github.com/primandproper/platform-go/v13/observability/tracing"
	"github.com/primandproper/platform-go/v13/testutils/containers/pgtest"

	"github.com/jackc/pgx/v5"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// The claim this file exists to check is one no unit test can make: that a
// statement issued through the native pgx pools is spanned, and that a statement
// issued through the derived database/sql handles is spanned exactly once
// despite both surfaces running on the same connections. Only a real server
// exercises the pgx stdlib driver's path into *pgx.Conn, which is where the
// double-tracing would otherwise happen.

// otelsqlScope is the instrumentation scope the database/sql spans carry, which
// is what tells them apart from the native ones sharing their name.
const otelsqlScope = "github.com/XSAM/otelsql"

// tracedClient stands a client up with a recorder of its own, so that subtests
// asserting on span counts do not have to read each other's spans out of a
// shared one.
func tracedClient(t *testing.T, connectionString string) (*Client, tracing.Provider, *tracetest.SpanRecorder) {
	t.Helper()

	provider, recorder := recordingTracerProvider(t)

	client, err := NewDatabaseClient(
		t.Context(),
		&testClientConfig{connectionString: connectionString},
		WithTracerProvider(provider),
	)
	must.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	return client, provider, recorder
}

// spansNamed collects the recorded spans with the given name, having first
// drained whatever the provider is still holding.
func spansNamed(
	t *testing.T,
	provider tracing.Provider,
	recorder *tracetest.SpanRecorder,
	name string,
) []sdktrace.ReadOnlySpan {
	t.Helper()

	must.NoError(t, provider.ForceFlush(context.Background()))

	var matched []sdktrace.ReadOnlySpan

	for _, span := range recorder.Ended() {
		if span.Name() == name {
			matched = append(matched, span)
		}
	}

	return matched
}

// statementAttribute reads whichever key the span reported the query text under.
func statementAttribute(span sdktrace.ReadOnlySpan) string {
	attributes := span.Attributes()
	for i := range attributes {
		if attributes[i].Key == "db.statement" || attributes[i].Key == "db.query.text" {
			return attributes[i].Value.AsString()
		}
	}

	return ""
}

func TestClient_TracesBothSurfaces(T *testing.T) {
	T.Parallel()

	pgtest.Run(T, func(_ context.Context, pg *pgtest.Instance) {
		T.Run("a native pool query is spanned once, under the client's own scope", func(t *testing.T) {
			t.Parallel()

			client, provider, recorder := tracedClient(t, pg.ConnectionString)

			var got int
			must.NoError(t, client.ReadPool().QueryRow(t.Context(), "SELECT 1").Scan(&got))
			test.EqOp(t, 1, got)

			spans := spansNamed(t, provider, recorder, pgxQuerySpanName)
			must.SliceLen(t, 1, spans)
			test.EqOp(t, tracingName, spans[0].InstrumentationScope().Name)
			test.EqOp(t, "SELECT 1", statementAttribute(spans[0]))
		})

		T.Run("a native batch is spanned, one event per statement", func(t *testing.T) {
			t.Parallel()

			client, provider, recorder := tracedClient(t, pg.ConnectionString)

			batch := &pgx.Batch{}
			batch.Queue("SELECT 1")
			batch.Queue("SELECT 2")

			must.NoError(t, client.WritePool().SendBatch(t.Context(), batch).Close())

			spans := spansNamed(t, provider, recorder, pgxBatchSpanName)
			must.SliceLen(t, 1, spans)
			test.EqOp(t, tracingName, spans[0].InstrumentationScope().Name)
			test.SliceLen(t, 2, spans[0].Events())
		})

		T.Run("a derived database/sql query is spanned once, by otelsql", func(t *testing.T) {
			t.Parallel()

			client, provider, recorder := tracedClient(t, pg.ConnectionString)

			var got int
			must.NoError(t, client.ReadDB().QueryRowContext(t.Context(), "SELECT 1").Scan(&got))
			test.EqOp(t, 1, got)

			// The pgx tracer sees this statement too — the stdlib driver runs it
			// on the same *pgx.Conn — and must decline to span it.
			spans := spansNamed(t, provider, recorder, pgxQuerySpanName)
			must.SliceLen(t, 1, spans)
			test.EqOp(t, otelsqlScope, spans[0].InstrumentationScope().Name)
		})

		T.Run("a derived exec keeps otelsql's own name and gains nothing else", func(t *testing.T) {
			t.Parallel()

			client, provider, recorder := tracedClient(t, pg.ConnectionString)

			_, err := client.Writer().ExecContext(t.Context(), "SELECT 1")
			must.NoError(t, err)

			test.SliceEmpty(t, spansNamed(t, provider, recorder, pgxQuerySpanName))

			spans := spansNamed(t, provider, recorder, "sql.conn.exec")
			must.SliceLen(t, 1, spans)
			test.EqOp(t, otelsqlScope, spans[0].InstrumentationScope().Name)
		})

		T.Run("a transaction on the derived surface is not doubled", func(t *testing.T) {
			t.Parallel()

			client, provider, recorder := tracedClient(t, pg.ConnectionString)

			must.NoError(t, client.WithTransaction(t.Context(), func(tx database.Tx) error {
				_, err := tx.ExecContext(t.Context(), "SELECT 1")

				return err
			}))

			// BEGIN, the statement, and COMMIT all run as pgx statements on the
			// context BeginTx marked, so none of them reach the native tracer.
			for _, span := range spansNamed(t, provider, recorder, pgxQuerySpanName) {
				test.EqOp(t, otelsqlScope, span.InstrumentationScope().Name)
			}
		})

		T.Run("a prepared statement on the derived surface is not doubled", func(t *testing.T) {
			t.Parallel()

			client, provider, recorder := tracedClient(t, pg.ConnectionString)

			stmt, err := client.ReadDB().PrepareContext(t.Context(), "SELECT 1")
			must.NoError(t, err)
			t.Cleanup(func() { _ = stmt.Close() })

			var got int
			must.NoError(t, stmt.QueryRowContext(t.Context()).Scan(&got))
			test.EqOp(t, 1, got)

			for _, span := range spansNamed(t, provider, recorder, pgxQuerySpanName) {
				test.EqOp(t, otelsqlScope, span.InstrumentationScope().Name)
			}

			// sql.conn.prepare is otelsql's name as well as this package's, which
			// is the point — what must not happen is one of each for the one
			// prepare.
			prepares := spansNamed(t, provider, recorder, pgxPrepareSpanName)
			must.SliceLen(t, 1, prepares)
			test.EqOp(t, otelsqlScope, prepares[0].InstrumentationScope().Name)
		})

		T.Run("a client given no tracer provider installs no pgx tracer", func(t *testing.T) {
			t.Parallel()

			client, err := NewDatabaseClient(t.Context(), &testClientConfig{connectionString: pg.ConnectionString})
			must.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })

			test.Nil(t, client.ReadPool().Config().ConnConfig.Tracer)
		})
	})
}
