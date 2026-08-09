package dataprivacy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"math/rand/v2"
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/primandproper/platform-go/v10/audit"
	"github.com/primandproper/platform-go/v10/clock"
	"github.com/primandproper/platform-go/v10/database"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
	"github.com/primandproper/platform-go/v10/panicking"
	"github.com/primandproper/platform-go/v10/retry"
	retrycfg "github.com/primandproper/platform-go/v10/retry/config"
	"github.com/primandproper/platform-go/v10/uploads"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// maxStoredErrorLength bounds a stored error rendering. A collector that
// returns a database error containing the row it choked on could otherwise
// write that row into the request record — which is to say, copy the subject's
// data into the table that records the request to delete it.
const maxStoredErrorLength = 1024

var (
	// ErrNoUploadManager indicates an export Worker with nowhere to write. It is
	// refused at construction: a worker that collects eleven domains and then
	// discovers it has no storage has already done all the expensive work and
	// still fails.
	ErrNoUploadManager = platformerrors.New("no dataprivacy upload manager configured")

	// ErrDocumentTooLarge indicates an assembled export past MaxDocumentBytes.
	ErrDocumentTooLarge = platformerrors.New("dataprivacy export document exceeds configured maximum")

	// ErrEverySectionFailed indicates an export in which no collector succeeded.
	//
	// A partial export is delivered with a manifest naming the gaps, because
	// most of somebody's data plus an honest account of the rest is worth
	// having. An export with no data at all is not a partial export — it is a
	// file asserting that nothing is held about a person, which is the one
	// wrong answer this package could give.
	ErrEverySectionFailed = platformerrors.New("no dataprivacy collector succeeded")

	// ErrInvalidFragment indicates a Collector that returned something that is
	// not valid JSON. It is caught before assembly rather than at read time,
	// because a malformed fragment would otherwise produce an artifact that
	// cannot be parsed at all — turning one domain's bug into a total loss.
	ErrInvalidFragment = platformerrors.New("dataprivacy collector returned invalid JSON")

	// ErrCollectorPanicked indicates a Collector that panicked. It is that
	// section's failure rather than the worker's death: a nil map access in one
	// domain should cost that domain's section, not every other request in the
	// batch.
	ErrCollectorPanicked = platformerrors.New("dataprivacy collector panicked")

	// ErrEraserPanicked indicates an Eraser that panicked. Unlike a collector
	// panic this aborts the whole erasure, because every eraser shares one
	// transaction and a panic mid-way through leaves no coherent partial state
	// to record.
	ErrEraserPanicked = platformerrors.New("dataprivacy eraser panicked")
)

// panicStackKey carries a contained panic's stack. Span-only: a stack trace is
// long, is attached to something already being reported as an error, and does
// not belong in every log aggregator's index.
const panicStackKey = "dataprivacy.panic_stack"

// Worker fulfills claimed requests. It owns a goroutine started by Run and
// stopped by Close.
type Worker struct {
	store    Store
	registry *Registry
	clock    clock.Clock
	o11y     observability.Observer
	logger   logging.Logger
	uploader uploads.UploadManager
	notifier Notifier
	recorder audit.Recorder
	actor    ActorResolver
	signer   func(ctx context.Context, req *Request) (string, time.Time)

	packager packager

	stop chan struct{}
	done chan struct{}

	completedCounter  metrics.Int64Counter
	failedCounter     metrics.Int64Counter
	sectionCounter    metrics.Int64Counter
	sectionErrCounter metrics.Int64Counter
	partialCounter    metrics.Int64Counter
	notifyErrCounter  metrics.Int64Counter
	claimErrCounter   metrics.Int64Counter
	erasedGauge       metrics.Int64Counter
	fulfillHist       metrics.Float64Histogram
	collectHist       metrics.Float64Histogram
	artifactHist      metrics.Float64Histogram

	tracerProvider  tracing.Provider
	metricsProvider metrics.Provider

	cfg WorkerConfig

	stopOnce sync.Once
}

// NewWorker builds a Worker. It does not start it; call Run.
//
// ctx is used to validate the config and is not retained — Run takes its own.
//
// A registry with no collectors and no erasers is refused. So is an export
// capability with no storage. Both would produce a worker that claims requests,
// does nothing useful, and reports success — and an erasure service that erases
// nothing while reporting success is the worst failure available here, because
// nobody goes looking for it.
func NewWorker(
	ctx context.Context,
	cfg *WorkerConfig,
	store Store,
	registry *Registry,
	opts ...WorkerOption,
) (*Worker, error) {
	if cfg == nil {
		return nil, platformerrors.New("nil dataprivacy worker config provided")
	}

	if store == nil {
		return nil, ErrNilStore
	}

	if registry == nil {
		return nil, platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil dataprivacy registry")
	}

	cfg.EnsureDefaults()

	w := &Worker{
		cfg:      *cfg,
		store:    store,
		registry: registry,
		clock:    clock.NewClock(),
		actor: func(context.Context) audit.Actor {
			return audit.Actor{ID: serviceName, Type: audit.ActorSystem}
		},
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}

	if err := w.cfg.ValidateWithContext(ctx); err != nil {
		return nil, platformerrors.Wrap(err, "validating dataprivacy worker config")
	}

	if len(registry.CollectorKeys()) == 0 && len(registry.EraserKeys()) == 0 {
		return nil, ErrNoCollectors
	}

	if len(registry.CollectorKeys()) > 0 && w.uploader == nil {
		return nil, ErrNoUploadManager
	}

	w.o11y = observability.NewObserver(serviceName, w.logger, w.tracerProvider)
	w.logger = w.o11y.Logger()

	if err := w.buildInstruments(); err != nil {
		return nil, err
	}

	return w, nil
}

// buildInstruments creates the Worker's metrics up front, so a misconfigured
// meter fails the constructor rather than the first cycle.
func (w *Worker) buildInstruments() error {
	mp := metrics.EnsureMetricsProvider(w.metricsProvider)

	var err error
	if w.completedCounter, err = mp.NewInt64Counter(serviceName + "_requests_completed"); err != nil {
		return platformerrors.Wrap(err, "creating requests completed counter")
	}
	if w.failedCounter, err = mp.NewInt64Counter(serviceName + "_requests_failed"); err != nil {
		return platformerrors.Wrap(err, "creating requests failed counter")
	}
	if w.sectionCounter, err = mp.NewInt64Counter(serviceName + "_sections_collected"); err != nil {
		return platformerrors.Wrap(err, "creating sections collected counter")
	}
	if w.sectionErrCounter, err = mp.NewInt64Counter(serviceName + "_section_failures"); err != nil {
		return platformerrors.Wrap(err, "creating section failures counter")
	}
	if w.partialCounter, err = mp.NewInt64Counter(serviceName + "_exports_partial"); err != nil {
		return platformerrors.Wrap(err, "creating partial exports counter")
	}
	if w.notifyErrCounter, err = mp.NewInt64Counter(serviceName + "_notification_failures"); err != nil {
		return platformerrors.Wrap(err, "creating notification failures counter")
	}
	if w.claimErrCounter, err = mp.NewInt64Counter(serviceName + "_claim_errors"); err != nil {
		return platformerrors.Wrap(err, "creating claim error counter")
	}
	if w.erasedGauge, err = mp.NewInt64Counter(serviceName + "_rows_erased"); err != nil {
		return platformerrors.Wrap(err, "creating rows erased counter")
	}
	if w.fulfillHist, err = mp.NewFloat64Histogram(serviceName + "_fulfillment_latency_ms"); err != nil {
		return platformerrors.Wrap(err, "creating fulfillment latency histogram")
	}
	if w.collectHist, err = mp.NewFloat64Histogram(serviceName + "_collector_latency_ms"); err != nil {
		return platformerrors.Wrap(err, "creating collector latency histogram")
	}
	if w.artifactHist, err = mp.NewFloat64Histogram(serviceName + "_artifact_bytes"); err != nil {
		return platformerrors.Wrap(err, "creating artifact size histogram")
	}

	return nil
}

// Run is the worker loop. Like webhooks.Worker.Run it takes no context: tied to
// a server context it would stop fulfilling while requests were still being
// submitted. The owner calls Close after the server has shut down.
//
// Run returns only after Close.
func (w *Worker) Run() {
	defer close(w.done)

	ctx := context.Background()

	ticker := w.clock.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.Chan():
			w.cycle(ctx)
		}
	}
}

// Close stops the worker and waits for the in-flight cycle to finish. Safe to
// call more than once.
//
// Unlike webhooks.Worker.Close there is no final cycle on the way out. A
// fulfillment can run for minutes and holds a lease that outlives the process,
// so the right thing at shutdown is to stop claiming and let the next replica
// pick the work up — not to start an export that will be killed halfway
// through.
func (w *Worker) Close(ctx context.Context) error {
	_, op := w.o11y.Begin(ctx)
	defer op.End()

	w.stopOnce.Do(func() { close(w.stop) })

	select {
	case <-w.done:
	case <-ctx.Done():
		return op.Error(ctx.Err(), "waiting for dataprivacy worker to drain")
	}

	return nil
}

// cycle claims one batch and fulfills it. Errors are logged and counted rather
// than returned: there is no caller to hand them to, and the next cycle retries.
func (w *Worker) cycle(ctx context.Context) {
	now := w.clock.Now().UTC()

	claimed, err := w.store.Claim(ctx, now, w.cfg.BatchSize, now.Add(w.cfg.LeaseDuration))
	if err != nil {
		w.claimErrCounter.Add(ctx, 1)
		w.logger.Error("claiming dataprivacy requests", err)

		return
	}

	if len(claimed) == 0 {
		return
	}

	ctx, op := w.o11y.Begin(ctx, observability.WithValue(claimedKey, len(claimed)))
	defer op.End()

	sem := make(chan struct{}, w.cfg.Concurrency)

	var wg sync.WaitGroup

	for _, req := range claimed {
		sem <- struct{}{}

		wg.Go(func() {
			defer func() { <-sem }()

			w.handle(ctx, req)
		})
	}

	wg.Wait()
}

// handle fulfills one request and records the outcome.
func (w *Worker) handle(ctx context.Context, req *Request) {
	startTime := time.Now()

	ctx, op := w.o11y.Begin(ctx, observability.WithValues(map[string]any{
		requestIDKey:   req.ID,
		requestTypeKey: string(req.Type),
		subjectIDKey:   req.Subject.ID,
		statusKey:      string(req.Status),
	}))
	defer op.End()

	// Bounded so a collector that hangs cannot hold the lease past its expiry
	// and let a second worker start the same request. The config validation
	// enforces LeaseDuration > FulfillmentTimeout for exactly this reason.
	//
	// The bounded context is deliberately not reused for the bookkeeping below:
	// on a fulfillment timeout it is already done, so every write made through it
	// — including the one recording the failure — is guaranteed to fail too. That
	// left the request in StatusProcessing with nothing to move it, which is the
	// wedge this separation exists to prevent.
	fulfillCtx, cancel := context.WithTimeout(ctx, w.cfg.FulfillmentTimeout)
	defer cancel()

	err := w.fulfill(fulfillCtx, req)

	w.fulfillHist.Record(ctx, float64(time.Since(startTime).Milliseconds()), requestTypeAttr(req.Type))

	if err != nil {
		w.recordFailure(ctx, req, err)

		return
	}

	w.completedCounter.Add(ctx, 1, requestTypeAttr(req.Type))

	if req.Partial() {
		w.partialCounter.Add(ctx, 1)
		op.Set(failureCountKey, len(req.Failures))
	}

	w.notify(ctx, req)
}

// fulfill dispatches on request type.
func (w *Worker) fulfill(ctx context.Context, req *Request) error {
	switch req.Type {
	case RequestExport:
		return w.fulfillExport(ctx, req)
	case RequestErasure:
		return w.fulfillErasure(ctx, req)
	default:
		// Unretryable: a request type this build does not implement will not
		// become implemented by waiting, and retrying it twenty-five times only
		// delays somebody noticing.
		return retry.Unretryable(platformerrors.Wrapf(ErrUnknownRequestType, "dataprivacy request type %q", req.Type))
	}
}

// fulfillExport collects, packages, stores, and records one export.
func (w *Worker) fulfillExport(ctx context.Context, req *Request) error {
	ctx, op := w.o11y.Begin(ctx)
	defer op.End()

	doc, err := w.collect(ctx, req)
	if err != nil {
		return err
	}

	stored, err := w.packager.encode(ctx, doc, req.ID)
	if err != nil {
		return err
	}

	if int64(len(stored)) > w.cfg.MaxDocumentBytes {
		return retry.Unretryable(platformerrors.Wrapf(
			ErrDocumentTooLarge,
			"dataprivacy export is %d bytes, limit is %d", len(stored), w.cfg.MaxDocumentBytes,
		))
	}

	ref := artifactPath(w.cfg.ArtifactPathPrefix, req.ID)

	if err = w.uploader.Save(ctx, ref, bytes.NewReader(stored),
		uploads.WithContentType(w.packager.contentType()),
		// Explicitly uncacheable. The object is a person's entire data
		// footprint behind an expiring URL, and a cache between the bucket and
		// the subject would keep serving it after the link expired and after
		// the sweeper deleted it.
		uploads.WithCacheControl("private, no-store"),
	); err != nil {
		return platformerrors.Wrap(err, "storing dataprivacy export artifact")
	}

	now := w.clock.Now().UTC()

	req.ArtifactRef = ref
	req.ArtifactBytes = int64(len(stored))
	req.ExpiresAt = now.Add(w.cfg.ArtifactTTL)
	req.Failures = doc.Manifest.Failures
	req.Status = StatusCompleted
	req.CompletedAt = &now

	w.artifactHist.Record(ctx, float64(len(stored)))

	// The reference and the size, not the contents. What is in the artifact is
	// the whole point of keeping it out of telemetry.
	op.Set(artifactRefKey, ref).Set(artifactSizeKey, req.ArtifactBytes)

	if err = w.store.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		if txErr := w.store.CompleteExport(ctx, q, req, now); txErr != nil {
			return txErr
		}

		return w.record(ctx, q, req, map[string]string{
			"artifact_bytes":  itoa(req.ArtifactBytes),
			"sections":        itoa(int64(len(doc.Data))),
			"failed_sections": itoa(int64(len(doc.Manifest.Failures))),
		})
	}); err != nil {
		// The object is written but the row does not say so. The sweeper only
		// deletes artifacts it can see a row for, so this leaves an orphan —
		// which is why the reference is derived from the request ID rather than
		// being random: a retry writes to the same key and overwrites it.
		return platformerrors.Wrap(err, "recording completed dataprivacy export")
	}

	return nil
}

// collect fans out over the registered collectors.
//
// Every collector gets its own timeout and its own error slot, and a failure is
// recorded against the key rather than propagated. That per-key isolation is
// the whole reason collection is a map of small interfaces instead of one
// method filling one shared struct: in the prior art a single domain returning
// an error aborted the aggregate, so a subject's export failed entirely because
// one unrelated table was slow.
func (w *Worker) collect(ctx context.Context, req *Request) (*Document, error) {
	ctx, op := w.o11y.Begin(ctx)
	defer op.End()

	keys := w.registry.CollectorKeys()

	op.Set(requestIDKey, req.ID).Set(sectionCountKey, len(keys))

	var (
		mu       sync.Mutex
		data     = make(map[string]json.RawMessage, len(keys))
		failures = map[string]string{}
		sem      = make(chan struct{}, w.cfg.CollectorConcurrency)
		wg       sync.WaitGroup
	)

	for _, key := range keys {
		sem <- struct{}{}

		wg.Go(func() {
			defer func() { <-sem }()

			fragment, err := w.collectOne(ctx, key, req.Subject)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failures[key] = truncateError(err)
				w.sectionErrCounter.Add(ctx, 1, sectionAttr(key))
				w.logger.WithValues(map[string]any{
					requestIDKey: req.ID,
					sectionKey:   key,
				}).Error("collecting dataprivacy export section", err)

				return
			}

			// A nil fragment is "nothing about this subject", which is a
			// complete answer and not an empty one. Omitting the section says
			// that; writing null would claim the domain holds a null.
			if len(fragment) > 0 {
				data[key] = fragment
				w.sectionCounter.Add(ctx, 1, sectionAttr(key))
			}
		})
	}

	wg.Wait()

	if len(data) == 0 && len(failures) > 0 {
		return nil, platformerrors.Wrapf(ErrEverySectionFailed, "%d of %d sections failed", len(failures), len(keys))
	}

	if len(failures) == 0 {
		failures = nil
	}

	return &Document{
		Data: data,
		Manifest: Manifest{
			Format:      DocumentFormat,
			RequestID:   req.ID,
			Subject:     req.Subject,
			GeneratedAt: w.clock.Now().UTC(),
			Sections:    sortedKeys(data),
			Failures:    failures,
		},
	}, nil
}

// collectOne runs a single collector under its own timeout and span.
//
// A collector that panics is converted into that section's failure rather than
// taking the worker down. It is somebody else's code running in our goroutine,
// and a nil map access in one domain should cost that domain's section — not
// every other request in the batch.
func (w *Worker) collectOne(ctx context.Context, key string, subject Subject) (json.RawMessage, error) {
	ctx, op := w.o11y.Begin(ctx,
		observability.WithValue(sectionKey, key),
		observability.WithValue(subjectIDKey, subject.ID),
	)
	defer op.End()

	collector, ok := w.registry.Collector(key)
	if !ok {
		return nil, platformerrors.Newf("no dataprivacy collector registered for %q", key)
	}

	ctx, cancel := context.WithTimeout(ctx, w.cfg.CollectorTimeout)
	defer cancel()

	var fragment json.RawMessage

	startTime := time.Now()

	err := panicking.Contain(func() error {
		var collectErr error
		fragment, collectErr = collector.Collect(ctx, subject)

		return collectErr
	})

	w.collectHist.Record(ctx, float64(time.Since(startTime).Milliseconds()), sectionAttr(key))

	if err != nil {
		return nil, op.Error(containedPanic(op, err, ErrCollectorPanicked), "collecting dataprivacy section")
	}

	// Validated here rather than trusted, because one domain returning
	// malformed bytes would otherwise make the whole artifact unparseable —
	// converting one domain's bug into a total loss for the subject.
	if len(fragment) > 0 && !json.Valid(fragment) {
		return nil, op.Error(platformerrors.Wrapf(ErrInvalidFragment, "section %q", key), "validating collected fragment")
	}

	return fragment, nil
}

// fulfillErasure runs every registered eraser and records the outcome, all in
// one transaction.
//
// Erasure is atomic across domains, and deliberately so. A partial erasure has
// no coherent meaning: a subject who is deleted from eight domains and present
// in three has not been erased, and cannot be told they have. The alternative —
// per-domain isolation, as collection uses — would leave the system in a state
// no status could describe. So an eraser's error aborts the whole thing and the
// request is retried intact.
func (w *Worker) fulfillErasure(ctx context.Context, req *Request) error {
	ctx, op := w.o11y.Begin(ctx)
	defer op.End()

	keys := w.registry.EraserKeys()

	op.Set(requestIDKey, req.ID).Set(sectionCountKey, len(keys))

	if len(keys) == 0 {
		return retry.Unretryable(ErrNoErasers)
	}

	now := w.clock.Now().UTC()

	err := w.store.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		var (
			deleted    int64
			anonymized int64
			retained   = map[string]string{}
		)

		// Serially, not concurrently. Every eraser shares one transaction, and
		// a *sql.Tx is a single connection: concurrent statements on it are a
		// data race the driver will either serialize or reject.
		for _, key := range keys {
			outcome, eraseErr := w.eraseOne(ctx, q, key, req.Subject)
			if eraseErr != nil {
				return eraseErr
			}

			deleted += outcome.Deleted
			anonymized += outcome.Anonymized

			for what, basis := range outcome.Retained {
				// Namespaced by eraser key so two domains retaining "invoices"
				// for different reasons do not overwrite each other.
				retained[key+"."+what] = basis
			}
		}

		req.Deleted = deleted
		req.Anonymized = anonymized
		req.Status = StatusCompleted
		req.CompletedAt = &now

		if len(retained) > 0 {
			req.Retained = retained
		}

		if txErr := w.store.CompleteErasure(ctx, q, req, now); txErr != nil {
			return txErr
		}

		return w.record(ctx, q, req, map[string]string{
			"deleted":    itoa(deleted),
			"anonymized": itoa(anonymized),
			"retained":   itoa(int64(len(retained))),
		})
	})
	if err != nil {
		return err
	}

	op.SetValues(map[string]any{
		deletedKey:    req.Deleted,
		anonymizedKey: req.Anonymized,
		retainedKey:   len(req.Retained),
	})

	w.erasedGauge.Add(ctx, req.Deleted+req.Anonymized)

	return nil
}

// eraseOne runs a single eraser, converting a panic into an error so that one
// domain's bug aborts the transaction cleanly rather than unwinding the worker
// with a half-applied erasure in flight.
func (w *Worker) eraseOne(
	ctx context.Context,
	q database.SQLQueryExecutor,
	key string,
	subject Subject,
) (ErasureOutcome, error) {
	ctx, op := w.o11y.Begin(ctx,
		observability.WithValue(sectionKey, key),
		observability.WithValue(subjectIDKey, subject.ID),
	)
	defer op.End()

	eraser, ok := w.registry.Eraser(key)
	if !ok {
		return ErasureOutcome{}, platformerrors.Newf("no dataprivacy eraser registered for %q", key)
	}

	var outcome ErasureOutcome

	err := panicking.Contain(func() error {
		var eraseErr error
		outcome, eraseErr = eraser.Erase(ctx, q, subject)

		return eraseErr
	})
	if err != nil {
		return ErasureOutcome{}, op.Error(containedPanic(op, err, ErrEraserPanicked), "erasing dataprivacy section %q", key)
	}

	op.Set(deletedKey, outcome.Deleted).Set(anonymizedKey, outcome.Anonymized)

	return outcome, nil
}

// containedPanic turns a panic that panicking.Contain caught into one of this
// package's sentinels, putting the stack on the span first — the wrapped
// sentinel no longer carries it. Anything that is not a contained panic is
// returned untouched.
func containedPanic(op observability.Operation, err, sentinel error) error {
	pe, ok := errors.AsType[*panicking.PanicError](err)
	if !ok {
		return err
	}

	op.SpanOnly(panicStackKey, string(pe.Stack))

	return platformerrors.Wrapf(sentinel, "%v", pe.Value)
}

// recordFailure releases the lease and either schedules a retry or gives up.
func (w *Worker) recordFailure(ctx context.Context, req *Request, cause error) {
	w.failedCounter.Add(ctx, 1, requestTypeAttr(req.Type))

	terminal := errors.Is(cause, retry.ErrUnretryable) || uint(req.Attempts) >= w.cfg.Backoff.MaxAttempts

	nextAttempt := w.clock.Now().UTC().Add(w.backoffFor(req.Attempts))

	logger := w.logger.WithValues(map[string]any{
		requestIDKey:   req.ID,
		requestTypeKey: string(req.Type),
		subjectIDKey:   req.Subject.ID,
		"attempts":     req.Attempts,
	})

	if err := w.store.Fail(ctx, req.ID, req.Attempts, nextAttempt, truncateError(cause), terminal); err != nil {
		// The lease still expires on its own, so the request is retried
		// regardless — just later than intended.
		logger.Error("recording dataprivacy request failure", err)

		return
	}

	if terminal {
		logger.Error("dataprivacy request failed permanently", cause)

		// Somebody is owed an answer and is not going to get one. Telling them
		// it failed is better than a status page that says "processing" until
		// the statutory window runs out.
		req.Status = StatusFailed
		req.LastError = truncateError(cause)

		w.notify(ctx, req)

		return
	}

	logger.WithValue("next_attempt", nextAttempt).Info("dataprivacy request failed, retry scheduled")
}

// notify tells the subject the request is done. A notification failure is
// logged and counted but never fails the request: the export exists and the
// erasure ran, and re-running either to retry an email would re-run every
// collector against the subject's data.
func (w *Worker) notify(ctx context.Context, req *Request) {
	if w.notifier == nil {
		return
	}

	notification := &Notification{Request: req}

	if req.Status == StatusCompleted && req.Type == RequestExport && w.signer != nil {
		notification.DownloadURL, notification.ExpiresAt = w.signer(ctx, req)
	}

	if err := w.notifier.Notify(ctx, notification); err != nil {
		w.notifyErrCounter.Add(ctx, 1, requestTypeAttr(req.Type))
		w.logger.WithValue(requestIDKey, req.ID).Error("notifying dataprivacy request subject", err)
	}
}

// record appends the completion audit entry inside the caller's transaction.
func (w *Worker) record(ctx context.Context, q database.SQLQueryExecutor, req *Request, metadata map[string]string) error {
	if w.recorder == nil {
		return nil
	}

	fields := map[string]string{
		"request_type": string(req.Type),
		"status":       string(req.Status),
		"subject_id":   req.Subject.ID,
		"subject_type": string(req.Subject.Type),
	}
	maps.Copy(fields, metadata)

	return w.recorder.Record(ctx, q, &audit.Entry{
		EventType:    audit.EventUpdated,
		ResourceType: auditResourceType,
		ResourceID:   req.ID,
		Actor:        w.actor(ctx),
		Scope:        req.Subject.Scope,
		Metadata:     fields,
		RecordedAt:   w.clock.Now().UTC(),
	})
}

// backoffFor computes the delay before a request's next attempt.
//
// The schedule comes from retrycfg.DelayFor, so this and anything using a
// retry.Policy grow their delays identically from the same Config. The wait is
// persisted as a timestamp rather than slept through, so it survives a restart,
// and the jitter is full rather than equal — several workers share this table,
// and spreading their next attempts across the whole window is what keeps them
// from re-colliding after one contended claim.
func (w *Worker) backoffFor(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}

	delay := float64(retrycfg.DelayFor(w.cfg.Backoff, uint(attempts)))

	if w.cfg.Backoff.UseJitter {
		// Full jitter. Not security-sensitive: this only decorrelates retry
		// timing between workers.
		delay *= rand.Float64() //nolint:gosec // jitter, not entropy
	}

	if delay < float64(time.Millisecond) {
		delay = float64(time.Millisecond)
	}

	return time.Duration(delay)
}

// artifactPath renders where an export is stored.
//
// The request ID is the filename rather than the subject's, deliberately.
// Object keys leak: they appear in access logs, in bucket listings, in URLs
// somebody pastes into a ticket. A path built from a subject identifier would
// make the storage layout itself a record of who has asked to be forgotten.
//
// It is also derived rather than random, so a retry after a failed completion
// overwrites the orphaned object instead of leaving one behind on every
// attempt.
func artifactPath(prefix, requestID string) string {
	return path.Join(prefix, requestID+".json")
}

// NewArtifactURLSigner builds the signer a Worker hands to WithWorkerURLSigner,
// so a completion notification can carry a working download link.
//
// It exists because the Worker cannot hold a Service — a Service is the thing
// that reads what the Worker writes — but the notification is only useful with
// a URL in it. The manager and TTL must be the ones the Service would use, or
// the subject gets a link into the wrong bucket.
//
// It declines to sign in exactly the cases Service.Download refuses: an
// encrypted artifact, and a provider that cannot sign. An empty URL is not an
// error here — the notification simply tells the subject their export is ready
// and to sign in for it, which is the correct message when a link cannot be
// handed out.
func NewArtifactURLSigner(
	manager uploads.UploadManager,
	ttl time.Duration,
	encrypted bool,
) func(ctx context.Context, req *Request) (string, time.Time) {
	signer, canSign := manager.(uploads.URLSigner)

	return func(ctx context.Context, req *Request) (string, time.Time) {
		if !canSign || encrypted || req.ArtifactRef == "" {
			return "", time.Time{}
		}

		url, err := signer.SignedURL(ctx, req.ArtifactRef, &uploads.SignedURLOptions{
			Method: http.MethodGet,
			Expiry: ttl,
		})
		if err != nil {
			return "", time.Time{}
		}

		return url, time.Now().UTC().Add(ttl)
	}
}

// sectionAttr labels a measurement with its section. Cardinality is bounded by
// the registry, which is a fixed list written at wiring time.
func sectionAttr(key string) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(sectionKey, key))
}

// truncateError renders an error for storage, bounded.
func truncateError(err error) string {
	if err == nil {
		return ""
	}

	return truncate(err.Error(), maxStoredErrorLength)
}

// truncate cuts s to at most limit bytes without splitting a rune, so a
// truncated error is still valid UTF-8 and still stores.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}

	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}

	return s[:limit]
}

// itoa renders an int64 for an audit entry's metadata, which is a string map.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
