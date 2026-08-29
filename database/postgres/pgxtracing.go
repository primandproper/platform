package postgres

import (
	"context"
	"os"
	"strings"

	"github.com/primandproper/platform-go/v13/observability/tracing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconvlegacy "go.opentelemetry.io/otel/semconv/v1.24.0"
	semconvstable "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

// Span names for statements issued natively through the pgx pools. They sit in
// otelsql's sql.* namespace, and reuse its exact names where the two surfaces
// have the same operation, because an operator reading a trace should see one
// database rather than two: a query is a query whether it arrived through
// Reader/Writer or through a pool taken from PgxAccess. The instrumentation
// scope tells them apart when they need telling apart — otelsql's spans are
// scoped to otelsql, these to this client's own name.
//
// pgx routes Query, QueryRow, and Exec through a single tracer hook and does
// not say which it was, so a native Exec lands under the query name. The
// database/sql surface keeps otelsql's sql.conn.exec, because there the driver
// knows.
const (
	pgxQuerySpanName    = "sql.conn.query"
	pgxPrepareSpanName  = "sql.conn.prepare"
	pgxBatchSpanName    = "sql.conn.batch"
	pgxCopyFromSpanName = "sql.conn.copy_from"

	// pgxBatchQueryEvent names the per-statement event on a batch span. pgx
	// reports each statement in a batch once, on completion, with no matching
	// start hook — there is no duration to hang a child span on, so each is an
	// event instead.
	pgxBatchQueryEvent = "sql.conn.batch.query"
)

// pgxSpanContextKey carries the span a Trace*Start hook opened through to the
// matching End hook. pgx hands the returned context back at the end of the
// call, so the span travels with the operation rather than in the tracer, which
// is shared by every connection in both pools.
type pgxSpanContextKey struct{}

var (
	_ pgx.QueryTracer    = (*pgxTracer)(nil)
	_ pgx.BatchTracer    = (*pgxTracer)(nil)
	_ pgx.CopyFromTracer = (*pgxTracer)(nil)
	_ pgx.PrepareTracer  = (*pgxTracer)(nil)
)

// pgxTracer is the pgx-side half of this client's tracing: it spans statements
// issued directly against the native pools, which the otelsql instrumentation
// on the derived database/sql handles cannot see.
//
// The two surfaces share one set of connections, so pgx's tracer hook also
// fires for every statement the database/sql layer issues — the stdlib driver
// runs them through the same *pgx.Conn. Those are already spanned by otelsql,
// so the tracer skips any call arriving on a context the derived surface
// marked; see derivedsurface.go.
type pgxTracer struct {
	tracer     tracing.Tracer
	attributes []attribute.KeyValue
	semconv    semconvMode
	logQueries bool
}

// newPgxTracer builds the tracer for the native pools, or nil when the client
// was given no tracer provider. Nil is the point: absent means noop here as
// everywhere else, and a nil tracer is left off pgx's connection config
// entirely, so an untraced client pays neither the context value nor the
// connector wrapper the marking needs.
func newPgxTracer(tracerProvider tracing.Provider, attributes []attribute.KeyValue, logQueries bool) *pgxTracer {
	if tracerProvider == nil {
		return nil
	}

	return &pgxTracer{
		tracer:     tracing.NewNamedTracer(tracerProvider, tracingName),
		attributes: attributes,
		semconv:    parseSemconvOptIn(os.Getenv(otelSemconvStabilityOptIn)),
		logQueries: logQueries,
	}
}

// otelSemconvStabilityOptIn is the OpenTelemetry environment variable deciding
// which generation of the database conventions an instrumentation emits.
const otelSemconvStabilityOptIn = "OTEL_SEMCONV_STABILITY_OPT_IN"

// semconvMode is which database semantic conventions the query text is reported
// under.
type semconvMode uint8

const (
	// semconvLegacy emits db.statement only, and is the default: it is what
	// OpenTelemetry's database conventions called the query text before they
	// stabilized.
	semconvLegacy semconvMode = iota
	// semconvStable emits db.query.text only.
	semconvStable
	// semconvDup emits both, for a migration with dashboards on either.
	semconvDup
)

// parseSemconvOptIn reads OTEL_SEMCONV_STABILITY_OPT_IN the way otelsql reads
// it, "database/dup" taking precedence over "database" wherever in the list it
// appears.
//
// otelsql parses the same variable in an internal package, which is why this is
// a second copy rather than a call. The copy is deliberate and it is the
// narrow kind: a native span that named the query text differently from the
// database/sql span beside it would read as a different database, which is the
// one thing this tracer exists not to do.
func parseSemconvOptIn(value string) semconvMode {
	mode := semconvLegacy

	for item := range strings.SplitSeq(value, ",") {
		switch strings.TrimSpace(item) {
		case "database/dup":
			return semconvDup
		case "database":
			mode = semconvStable
		}
	}

	return mode
}

// statementAttributes reports the query text under whichever keys the mode asks
// for.
func (m semconvMode) statementAttributes(statement string) []attribute.KeyValue {
	switch m {
	case semconvStable:
		return []attribute.KeyValue{semconvstable.DBQueryTextKey.String(statement)}
	case semconvDup:
		return []attribute.KeyValue{
			semconvlegacy.DBStatementKey.String(statement),
			semconvstable.DBQueryTextKey.String(statement),
		}
	case semconvLegacy:
		return []attribute.KeyValue{semconvlegacy.DBStatementKey.String(statement)}
	default:
		return []attribute.KeyValue{semconvlegacy.DBStatementKey.String(statement)}
	}
}

// start opens a client span for a native statement, unless the call reached pgx
// through the derived database/sql surface, which otelsql has already spanned.
func (t *pgxTracer) start(ctx context.Context, name, statement string) context.Context {
	if fromDerivedSurface(ctx) {
		return ctx
	}

	ctx, span := t.tracer.StartCustomSpan(ctx, name, trace.WithSpanKind(trace.SpanKindClient))

	if span.IsRecording() {
		span.SetAttributes(t.spanAttributes(statement)...)
	}

	return context.WithValue(ctx, pgxSpanContextKey{}, span)
}

// spanAttributes builds a span's attributes: the client-wide set every span on
// either surface carries, plus the query text when the config opts into it.
func (t *pgxTracer) spanAttributes(statement string) []attribute.KeyValue {
	attributes := make([]attribute.KeyValue, 0, len(t.attributes)+2)
	attributes = append(attributes, t.attributes...)

	if t.logQueries && statement != "" {
		attributes = append(attributes, t.semconv.statementAttributes(statement)...)
	}

	return attributes
}

// end closes the span start opened, if it opened one.
func (t *pgxTracer) end(ctx context.Context, err error) {
	span, ok := pgxSpanFromContext(ctx)
	if !ok {
		return
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "")
	}

	span.End()
}

// pgxSpanFromContext recovers the span start stashed. A missing span means the
// statement came through the derived surface and was never spanned here.
func pgxSpanFromContext(ctx context.Context) (tracing.Span, bool) {
	if ctx == nil {
		return nil, false
	}

	span, ok := ctx.Value(pgxSpanContextKey{}).(tracing.Span)

	return span, ok && span != nil
}

// TraceQueryStart satisfies pgx.QueryTracer. It covers Query, QueryRow, and
// Exec alike.
func (t *pgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return t.start(ctx, pgxQuerySpanName, data.SQL)
}

// TraceQueryEnd satisfies pgx.QueryTracer. For Query it fires when the caller
// closes the rows, so the span measures the whole read rather than the round
// trip that began it.
func (t *pgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	t.end(ctx, data.Err)
}

// TraceBatchStart satisfies pgx.BatchTracer. The batch's statements are events
// on the span rather than its name, which would otherwise vary with whatever
// the caller queued.
func (t *pgxTracer) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return t.start(ctx, pgxBatchSpanName, "")
}

// TraceBatchQuery satisfies pgx.BatchTracer, recording one statement of a batch.
func (t *pgxTracer) TraceBatchQuery(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchQueryData) {
	span, ok := pgxSpanFromContext(ctx)
	if !ok || !span.IsRecording() {
		return
	}

	var opts []trace.EventOption
	if t.logQueries && data.SQL != "" {
		opts = append(opts, trace.WithAttributes(t.semconv.statementAttributes(data.SQL)...))
	}

	span.AddEvent(pgxBatchQueryEvent, opts...)

	// The batch's own end carries the first failure, but a statement that failed
	// midway is the one an operator is looking for.
	if data.Err != nil {
		span.RecordError(data.Err)
	}
}

// TraceBatchEnd satisfies pgx.BatchTracer.
func (t *pgxTracer) TraceBatchEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceBatchEndData) {
	t.end(ctx, data.Err)
}

// TraceCopyFromStart satisfies pgx.CopyFromTracer.
func (t *pgxTracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromStartData) context.Context {
	return t.start(ctx, pgxCopyFromSpanName, copyFromStatement(data))
}

// TraceCopyFromEnd satisfies pgx.CopyFromTracer.
func (t *pgxTracer) TraceCopyFromEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceCopyFromEndData) {
	t.end(ctx, data.Err)
}

// copyFromStatement renders the COPY a bulk load runs, so that the query text
// attribute on a copy span holds a statement rather than a table name under a
// key that promises SQL.
func copyFromStatement(data pgx.TraceCopyFromStartData) string {
	return "COPY " + data.TableName.Sanitize() + " (" + strings.Join(data.ColumnNames, ", ") + ") FROM STDIN"
}

// TracePrepareStart satisfies pgx.PrepareTracer. pgx calls it for an explicit
// Prepare, not for the statement cache's implicit ones, so these spans are as
// rare as the caller's own Prepare calls.
func (t *pgxTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareStartData) context.Context {
	return t.start(ctx, pgxPrepareSpanName, data.SQL)
}

// TracePrepareEnd satisfies pgx.PrepareTracer.
func (t *pgxTracer) TracePrepareEnd(ctx context.Context, _ *pgx.Conn, data pgx.TracePrepareEndData) {
	t.end(ctx, data.Err)
}
