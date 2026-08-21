package writes

import (
	"context"
	"slices"

	"github.com/primandproper/platform-go/v12/database"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/observability"
	"github.com/primandproper/platform-go/v12/observability/metrics"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// instrumentPrefix is the name the writer's instruments are built under, and
// the prefix of the attributes it stamps on its span. See metrics.OperationSet.
const instrumentPrefix = "database_writes"

// spanName is what a write's transaction is called in a trace.
//
// It is spelled out rather than resolved from the calling frame because the
// frame is Do, and a trace where every write is called "Do" tells a reader
// nothing they could not have guessed. The repository method above it is the
// span with the useful name, and this one nests inside it.
const spanName = instrumentPrefix + ".do"

// Attribute keys and stage values.
//
// The stage separates the two failures that arrive at the same place and mean
// different things: the domain's own statement failing is a bug in one query,
// and a hook failing is a write path about to stop accepting writes.
const (
	stageAttribute   = instrumentPrefix + ".stage"
	changesAttribute = instrumentPrefix + ".changes"

	// stageTransaction is a failure that belongs to neither the write nor a
	// hook: beginning the transaction, or committing it.
	stageTransaction = "transaction"
	// stageWrite is a failure returned by the caller's own write function.
	stageWrite = "write"
	// stageChange is a write that reported a change nothing could record.
	stageChange = "change"
	// stageHook is a failure returned by a hook.
	stageHook = "hook"
)

// Write is the caller's half of Do: the statements of one write, run against
// the transaction Do opened, reporting what they did.
//
// It returns its changes rather than being handed them because the identity a
// hook needs is often only known once the statements have run — an id minted
// during a create, the owner read off a row before it is archived, one Change
// per row of a cascade. Returning none is legitimate and commits: a write that
// matched nothing it wanted to announce is still a write.
type Write func(ctx context.Context, exec database.SQLQueryExecutor) ([]Change, error)

// Writer runs writes in a transaction and the application's hooks inside it.
//
// It is a concrete type and there is deliberately no Writer interface here. A
// repository that depends on one wants the single method it calls, declared on
// its own side where the compiler can check it uses no more than it asked for.
//
// One Writer per application is the expected shape — the hooks are the
// application's conventions, not a domain's — but nothing stops a domain with
// different conventions holding its own.
//
// It is not database.Client.Writer, which is that client's executor for single
// non-transactional statements. This is the opposite case: the multi-statement
// work that executor exists to keep out.
type Writer struct {
	client   database.Client
	observer observability.Observer

	instruments *metrics.OperationSet
	// stages holds the attribute set each failure stage is counted under, built
	// once so that no call site assembles a different one.
	stages map[string]metric.MeasurementOption

	hooks []Hook
}

// New builds a Writer over a database client.
//
// The hooks are fixed at construction rather than passed per call, because they
// are the property this exists to make uniform: a hook a call site can decline
// to pass is a hook eighty-three call sites can forget.
func New(client database.Client, opts ...Option) (*Writer, error) {
	if client == nil {
		return nil, platformerrors.ErrNilInputParameter
	}

	var settings options

	for _, opt := range opts {
		if opt != nil {
			opt(&settings)
		}
	}

	instruments, err := metrics.NewOperationSet(settings.metricsProvider, instrumentPrefix)
	if err != nil {
		return nil, platformerrors.Wrap(err, "instrumenting writes")
	}

	stages := make(map[string]metric.MeasurementOption, 4)
	for _, stage := range []string{stageTransaction, stageWrite, stageChange, stageHook} {
		stages[stage] = metric.WithAttributes(attribute.String(stageAttribute, stage))
	}

	return &Writer{
		client:      client,
		hooks:       slices.Clone(settings.hooks),
		instruments: instruments,
		stages:      stages,
		observer:    observability.NewObserver(instrumentPrefix, settings.logger, settings.tracerProvider),
	}, nil
}

// Do runs write inside one transaction, then every hook against every change it
// reported, in that same transaction.
//
// The order is the write's statements, then — for each change in the order it
// was reported — every hook in registration order. The first error anywhere
// stops the rest and rolls the whole thing back, so a row and the entry that
// describes it are never separately durable.
//
// The write's own error is returned unwrapped. It came from the caller's code,
// which has already described it in its own words, and ErrNoRowsAffected in
// particular has to arrive at the service that maps it intact. Failures this
// package produces — an unrecordable change, a hook — are logged and wrapped
// here, because until now nobody has said anything about them.
func (w *Writer) Do(ctx context.Context, write Write) (err error) {
	if write == nil {
		return platformerrors.ErrNilInputParameter
	}

	ctx, op := w.observer.BeginCustom(ctx, spanName)

	// The stage a failure is counted under is decided inside the transaction and
	// read after it returns, so that an error from BeginTx or Commit — which the
	// closure never sees — is still counted as something rather than as the last
	// stage that happened to run.
	stage := stageTransaction

	stop := op.Time(ctx, nil, w.instruments.Latency)

	w.instruments.Attempt(ctx)

	// Failures are counted at the return rather than at each of the sites that
	// can produce one: the site somebody forgets is not a missing metric but an
	// error rate that reads lower than it is, on the path that failed.
	defer func() {
		stop()

		if err != nil {
			op.SpanOnly(stageAttribute, stage)
			w.instruments.Failed(ctx, w.stages[stage])
		}

		op.End()
	}()

	return w.client.WithTransaction(ctx, func(exec database.SQLQueryExecutor) error {
		changes, writeErr := write(ctx, exec)
		if writeErr != nil {
			stage = stageWrite

			return writeErr
		}

		op.SpanOnly(changesAttribute, len(changes))

		// Every change is validated before any hook runs, so a write that
		// reported one unrecordable change does not leave the hooks having
		// recorded a prefix of what it did. The transaction would roll back
		// either way; what this buys is that the error names the change rather
		// than whichever hook happened to choke on it first.
		for i := range changes {
			if validationErr := changes[i].Validate(); validationErr != nil {
				stage = stageChange

				return op.Error(validationErr, "validating reported change")
			}
		}

		for i := range changes {
			change := &changes[i]

			for _, hook := range w.hooks {
				if hookErr := hook(ctx, exec, change); hookErr != nil {
					stage = stageHook

					return op.Error(hookErr, "running hook for %s %s %s", change.Op, change.Resource, change.ID)
				}
			}
		}

		return nil
	})
}
