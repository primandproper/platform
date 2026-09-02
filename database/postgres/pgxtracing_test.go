package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/primandproper/platform-go/v14/observability/tracing"
	tracingnoop "github.com/primandproper/platform-go/v14/observability/tracing/noop"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// recordingTracerProvider builds a tracing.Provider that keeps every span it is
// given, so a test can read back what the pgx tracer emitted.
func recordingTracerProvider(t *testing.T) (tracing.Provider, *tracetest.SpanRecorder) {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))

	t.Cleanup(func() {
		must.NoError(t, provider.Shutdown(context.Background()))
	})

	return provider, recorder
}

// buildRecordingPgxTracer is newPgxTracer against a recording provider, with the
// semantic-convention mode pinned so the assertions do not depend on the
// environment the suite happens to run in.
func buildRecordingPgxTracer(t *testing.T, logQueries bool) (*pgxTracer, *tracetest.SpanRecorder) {
	t.Helper()

	provider, recorder := recordingTracerProvider(t)

	tracer := newPgxTracer(provider, spanAttributes, logQueries)
	must.NotNil(t, tracer)
	tracer.semconv = semconvLegacy

	return tracer, recorder
}

func attributeValue(t *testing.T, span sdktrace.ReadOnlySpan, key attribute.Key) (attribute.Value, bool) {
	t.Helper()

	attributes := span.Attributes()
	for i := range attributes {
		if attributes[i].Key == key {
			return attributes[i].Value, true
		}
	}

	return attribute.Value{}, false
}

func TestNewPgxTracer(T *testing.T) {
	T.Parallel()

	T.Run("absent tracer provider means no tracer at all", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, newPgxTracer(nil, spanAttributes, true))
	})

	T.Run("a provider yields a tracer carrying the client's attributes", func(t *testing.T) {
		t.Parallel()

		tracer := newPgxTracer(tracingnoop.NewTracerProvider(), spanAttributes, false)
		must.NotNil(t, tracer)
		test.Eq(t, spanAttributes, tracer.attributes)
		test.False(t, tracer.logQueries)
	})
}

func TestParseSemconvOptIn(T *testing.T) {
	T.Parallel()

	cases := map[string]semconvMode{
		"":                          semconvLegacy,
		"http":                      semconvLegacy,
		"database":                  semconvStable,
		"  database  ":              semconvStable,
		"database/dup":              semconvDup,
		"http,database":             semconvStable,
		"database,database/dup":     semconvDup,
		"database/dup,database":     semconvDup,
		"http/dup,database/dup,foo": semconvDup,
	}

	for value, expected := range cases {
		T.Run(value, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, expected, parseSemconvOptIn(value))
		})
	}
}

func TestSemconvMode_statementAttributes(T *testing.T) {
	T.Parallel()

	T.Run("legacy reports the pre-stable key", func(t *testing.T) {
		t.Parallel()

		attrs := semconvLegacy.statementAttributes("SELECT 1")
		must.SliceLen(t, 1, attrs)
		test.EqOp(t, attribute.Key("db.statement"), attrs[0].Key)
		test.EqOp(t, "SELECT 1", attrs[0].Value.AsString())
	})

	T.Run("stable reports the stable key", func(t *testing.T) {
		t.Parallel()

		attrs := semconvStable.statementAttributes("SELECT 1")
		must.SliceLen(t, 1, attrs)
		test.EqOp(t, attribute.Key("db.query.text"), attrs[0].Key)
	})

	T.Run("dup reports both", func(t *testing.T) {
		t.Parallel()

		attrs := semconvDup.statementAttributes("SELECT 1")
		must.SliceLen(t, 2, attrs)
		test.EqOp(t, attribute.Key("db.statement"), attrs[0].Key)
		test.EqOp(t, attribute.Key("db.query.text"), attrs[1].Key)
	})
}

func TestPgxTracer_Query(T *testing.T) {
	T.Parallel()

	T.Run("spans a native query under the standard path's conventions", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{CommandTag: pgconn.NewCommandTag("SELECT 1")})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)
		test.EqOp(t, pgxQuerySpanName, spans[0].Name())
		test.EqOp(t, trace.SpanKindClient, spans[0].SpanKind())
		test.EqOp(t, codes.Unset, spans[0].Status().Code)

		service, ok := attributeValue(t, spans[0], "service.name")
		must.True(t, ok)
		test.EqOp(t, "database", service.AsString())

		statement, ok := attributeValue(t, spans[0], "db.statement")
		must.True(t, ok)
		test.EqOp(t, "SELECT 1", statement.AsString())
	})

	T.Run("omits the query text when the config opted out", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, false)

		ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT secret"})
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)

		_, ok := attributeValue(t, spans[0], "db.statement")
		test.False(t, ok)
	})

	T.Run("records the failure on the span", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)
		expected := errors.New("relation does not exist")

		ctx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: expected})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)
		test.EqOp(t, codes.Error, spans[0].Status().Code)
		must.SliceLen(t, 1, spans[0].Events())
		test.EqOp(t, "exception", spans[0].Events()[0].Name)
	})

	T.Run("skips a statement the derived database/sql surface already spanned", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		marked := markDerivedSurface(t.Context())

		ctx := tracer.TraceQueryStart(marked, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
		test.EqOp(t, marked, ctx)

		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("also ignored")})

		test.SliceEmpty(t, recorder.Ended())
	})
}

func TestPgxTracer_Batch(T *testing.T) {
	T.Parallel()

	T.Run("one span for the batch with an event per statement", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		ctx := tracer.TraceBatchStart(t.Context(), nil, pgx.TraceBatchStartData{})
		tracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{SQL: "INSERT INTO a VALUES (1)"})
		tracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{SQL: "INSERT INTO a VALUES (2)"})
		tracer.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)
		test.EqOp(t, pgxBatchSpanName, spans[0].Name())

		events := spans[0].Events()
		must.SliceLen(t, 2, events)
		test.EqOp(t, pgxBatchQueryEvent, events[0].Name)
		test.EqOp(t, "INSERT INTO a VALUES (1)", events[0].Attributes[0].Value.AsString())

		// The batch is not the statement, so the span itself names no SQL.
		_, ok := attributeValue(t, spans[0], "db.statement")
		test.False(t, ok)
	})

	T.Run("a statement that failed midway is recorded on the batch span", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, false)

		ctx := tracer.TraceBatchStart(t.Context(), nil, pgx.TraceBatchStartData{})
		tracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{SQL: "boom", Err: errors.New("boom")})
		tracer.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{Err: errors.New("boom")})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)
		test.EqOp(t, codes.Error, spans[0].Status().Code)

		// One event for the statement, one for the failure it reported, and one
		// for the batch's own.
		events := spans[0].Events()
		must.SliceLen(t, 3, events)
		test.EqOp(t, pgxBatchQueryEvent, events[0].Name)
		test.SliceEmpty(t, events[0].Attributes)
	})

	T.Run("skips a batch from the derived surface", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		ctx := tracer.TraceBatchStart(markDerivedSurface(t.Context()), nil, pgx.TraceBatchStartData{})
		tracer.TraceBatchQuery(ctx, nil, pgx.TraceBatchQueryData{SQL: "SELECT 1"})
		tracer.TraceBatchEnd(ctx, nil, pgx.TraceBatchEndData{})

		test.SliceEmpty(t, recorder.Ended())
	})
}

func TestPgxTracer_CopyFrom(T *testing.T) {
	T.Parallel()

	T.Run("names the copy it runs", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		ctx := tracer.TraceCopyFromStart(t.Context(), nil, pgx.TraceCopyFromStartData{
			TableName:   pgx.Identifier{"public", "users"},
			ColumnNames: []string{"id", "name"},
		})
		tracer.TraceCopyFromEnd(ctx, nil, pgx.TraceCopyFromEndData{})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)
		test.EqOp(t, pgxCopyFromSpanName, spans[0].Name())

		statement, ok := attributeValue(t, spans[0], "db.statement")
		must.True(t, ok)
		test.EqOp(t, `COPY "public"."users" (id, name) FROM STDIN`, statement.AsString())
	})
}

func TestPgxTracer_Prepare(T *testing.T) {
	T.Parallel()

	T.Run("spans an explicit prepare", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		ctx := tracer.TracePrepareStart(t.Context(), nil, pgx.TracePrepareStartData{Name: "s1", SQL: "SELECT 1"})
		tracer.TracePrepareEnd(ctx, nil, pgx.TracePrepareEndData{AlreadyPrepared: true})

		spans := recorder.Ended()
		must.SliceLen(t, 1, spans)
		test.EqOp(t, pgxPrepareSpanName, spans[0].Name())

		statement, ok := attributeValue(t, spans[0], "db.statement")
		must.True(t, ok)
		test.EqOp(t, "SELECT 1", statement.AsString())
	})

	T.Run("a prepare nested inside a query ends its own span", func(t *testing.T) {
		t.Parallel()

		tracer, recorder := buildRecordingPgxTracer(t, true)

		queryCtx := tracer.TraceQueryStart(t.Context(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
		prepareCtx := tracer.TracePrepareStart(queryCtx, nil, pgx.TracePrepareStartData{SQL: "SELECT 1"})
		tracer.TracePrepareEnd(prepareCtx, nil, pgx.TracePrepareEndData{})
		tracer.TraceQueryEnd(queryCtx, nil, pgx.TraceQueryEndData{})

		spans := recorder.Ended()
		must.SliceLen(t, 2, spans)
		test.EqOp(t, pgxPrepareSpanName, spans[0].Name())
		test.EqOp(t, pgxQuerySpanName, spans[1].Name())
		test.EqOp(t, spans[1].SpanContext().SpanID(), spans[0].Parent().SpanID())
	})
}

func TestPgxSpanFromContext(T *testing.T) {
	T.Parallel()

	T.Run("reports nothing for a context that never held a span", func(t *testing.T) {
		t.Parallel()

		_, ok := pgxSpanFromContext(t.Context())
		test.False(t, ok)
	})

	T.Run("reports nothing for a nil context", func(t *testing.T) {
		t.Parallel()

		//nolint:staticcheck // the nil guard is the thing under test.
		_, ok := pgxSpanFromContext(nil)
		test.False(t, ok)
	})
}
