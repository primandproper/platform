package dataprivacy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"

	"github.com/primandproper/platform-go/v9/audit"
	"github.com/primandproper/platform-go/v9/clock"
	"github.com/primandproper/platform-go/v9/compression"
	"github.com/primandproper/platform-go/v9/cryptography/encryption"
	"github.com/primandproper/platform-go/v9/database"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/filtering"
	"github.com/primandproper/platform-go/v9/identifiers"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
	"github.com/primandproper/platform-go/v9/uploads"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// auditResourceType is what this package's audit entries are about.
const auditResourceType = "dataprivacy_request"

var _ Service = (*service)(nil)

// service is the request state machine.
type service struct {
	store    Store
	clock    clock.Clock
	o11y     observability.Observer
	logger   logging.Logger
	uploader uploads.UploadManager
	recorder audit.Recorder
	actor    ActorResolver

	packager packager

	submittedCounter metrics.Int64Counter
	confirmedCounter metrics.Int64Counter
	cancelledCounter metrics.Int64Counter
	downloadCounter  metrics.Int64Counter

	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider

	cfg ServiceConfig
}

// ActorResolver names the principal responsible for an action, for the audit
// entry this package writes.
//
// It reads from the context because Submit's signature belongs to the subject,
// not to whoever is acting on their behalf — and those differ in exactly the
// case that matters. A support agent running an export for a customer is the
// event worth recording, and "who exported this person's data" is not
// answerable from the Subject alone.
//
// Without one, actions are attributed to audit.ActorSystem, which is honest
// for a self-service portal and misleading for a staff tool.
type ActorResolver func(ctx context.Context) audit.Actor

// ServiceOption configures a Service.
type ServiceOption func(*service)

// WithServiceClock swaps the clock stamping submission, deadline, and expiry.
func WithServiceClock(c clock.Clock) ServiceOption {
	return func(s *service) {
		if c != nil {
			s.clock = c
		}
	}
}

// WithServiceLogger attaches a logger.
func WithServiceLogger(logger logging.Logger) ServiceOption {
	return func(s *service) {
		s.logger = logger
	}
}

// WithServiceTracerProvider attaches a tracer provider.
func WithServiceTracerProvider(tracerProvider tracing.TracerProvider) ServiceOption {
	return func(s *service) {
		s.tracerProvider = tracerProvider
	}
}

// WithServiceMetricsProvider attaches a metrics provider.
func WithServiceMetricsProvider(metricsProvider metrics.Provider) ServiceOption {
	return func(s *service) {
		s.metricsProvider = metricsProvider
	}
}

// WithServiceUploadManager supplies the storage artifacts are read from.
//
// It must be the same storage, and the same path prefix, that the Worker writes
// to. Nothing here can check that, and a mismatch surfaces as an artifact that
// exists in the bucket and cannot be found by the service that promised it.
func WithServiceUploadManager(manager uploads.UploadManager) ServiceOption {
	return func(s *service) {
		if manager != nil {
			s.uploader = manager
		}
	}
}

// WithServiceCompressor supplies the compressor artifacts were written with. It
// must match the Worker's, or Open returns garbage.
func WithServiceCompressor(compressor compression.Compressor) ServiceOption {
	return func(s *service) {
		if compressor != nil {
			s.packager.compressor = compressor
		}
	}
}

// WithServiceDecryptor supplies the decryptor for artifacts written encrypted.
// It must match the Worker's encryptor.
//
// Setting it also disables Download: see ErrArtifactEncrypted.
func WithServiceDecryptor(decryptor encryption.Decryptor) ServiceOption {
	return func(s *service) {
		if decryptor != nil {
			s.packager.decryptor = decryptor
			// Recorded so Download can refuse rather than hand out a link to
			// ciphertext. The Service never encrypts — only the Worker does —
			// but a configured decryptor is proof that the Worker encrypts, and
			// is the only evidence of that available on this side.
			s.packager.encryptor = encryptorPresent{}
		}
	}
}

// encryptorPresent is a marker recording that artifacts are encrypted, without
// the Service holding an encryptor it would never use.
type encryptorPresent struct{}

func (encryptorPresent) Encrypt(context.Context, string) (string, error) {
	return "", platformerrors.New("dataprivacy service does not encrypt")
}

// WithServiceAuditRecorder attaches the audit log this package writes to.
//
// Every submission and every state change it drives is recorded. That is not
// decoration: an export artifact is the most sensitive object an application
// produces, and a system that can produce one without leaving a record of who
// asked has a data exfiltration path with no alarm on it.
func WithServiceAuditRecorder(recorder audit.Recorder) ServiceOption {
	return func(s *service) {
		if recorder != nil {
			s.recorder = recorder
		}
	}
}

// WithActorResolver supplies the principal recorded in audit entries.
func WithActorResolver(resolver ActorResolver) ServiceOption {
	return func(s *service) {
		if resolver != nil {
			s.actor = resolver
		}
	}
}

// NewService builds a Service.
//
// ctx is used to validate the config and is not retained.
func NewService(ctx context.Context, cfg *ServiceConfig, store Store, opts ...ServiceOption) (Service, error) {
	if cfg == nil {
		return nil, platformerrors.New("nil dataprivacy service config provided")
	}

	if store == nil {
		return nil, ErrNilStore
	}

	cfg.EnsureDefaults()

	s := &service{
		cfg:   *cfg,
		store: store,
		clock: clock.NewClock(),
		actor: func(context.Context) audit.Actor {
			return audit.Actor{ID: serviceName, Type: audit.ActorSystem}
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	if err := s.cfg.ValidateWithContext(ctx); err != nil {
		return nil, platformerrors.Wrap(err, "validating dataprivacy service config")
	}

	s.o11y = observability.NewObserver(serviceName, s.logger, s.tracerProvider)
	s.logger = s.o11y.Logger()

	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error
	if s.submittedCounter, err = mp.NewInt64Counter(serviceName + "_requests_submitted"); err != nil {
		return nil, platformerrors.Wrap(err, "creating requests submitted counter")
	}
	if s.confirmedCounter, err = mp.NewInt64Counter(serviceName + "_requests_confirmed"); err != nil {
		return nil, platformerrors.Wrap(err, "creating requests confirmed counter")
	}
	if s.cancelledCounter, err = mp.NewInt64Counter(serviceName + "_requests_cancelled"); err != nil {
		return nil, platformerrors.Wrap(err, "creating requests cancelled counter")
	}
	if s.downloadCounter, err = mp.NewInt64Counter(serviceName + "_artifact_downloads"); err != nil {
		return nil, platformerrors.Wrap(err, "creating artifact downloads counter")
	}

	return s, nil
}

func (s *service) Submit(ctx context.Context, subject Subject, t RequestType) (*Request, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	op.SetValues(map[string]any{
		subjectIDKey:    subject.ID,
		subjectTypeKey:  string(subject.Type),
		subjectScopeKey: subject.Scope,
		requestTypeKey:  string(t),
	})

	if err := subject.validate(); err != nil {
		return nil, op.Error(err, "validating dataprivacy subject")
	}

	if !t.Valid() {
		return nil, op.Error(platformerrors.Wrapf(ErrUnknownRequestType, "dataprivacy request type %q", t), "validating request type")
	}

	now := s.clock.Now().UTC()

	req := &Request{
		ID:          identifiers.New(),
		Type:        t,
		Subject:     subject,
		Status:      StatusPending,
		RequestedAt: now,
		DueAt:       now.Add(s.cfg.responseWindow(t)),
	}

	// Only erasure is ever held for confirmation. An export is reversible in
	// the only sense that matters — it can be expired and re-run — so making a
	// subject confirm one buys nothing and costs them a round trip.
	if t == RequestErasure && s.cfg.ConfirmationWindow > 0 {
		req.Status = StatusAwaitingConfirmation
		req.ExpiresAt = now.Add(s.cfg.ConfirmationWindow)
	}

	op.Set(requestIDKey, req.ID).Set(statusKey, string(req.Status))

	if err := s.store.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		if err := s.store.Save(ctx, q, req); err != nil {
			return err
		}

		return s.record(ctx, q, req, audit.EventCreated, nil)
	}); err != nil {
		return nil, op.Error(err, "submitting dataprivacy request")
	}

	s.submittedCounter.Add(ctx, 1, requestTypeAttr(t))

	return req, nil
}

func (s *service) Get(ctx context.Context, requestID string) (*Request, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	op.Set(requestIDKey, requestID)

	req, err := s.store.Get(ctx, requestID)
	if err != nil {
		return nil, op.Error(err, "reading dataprivacy request")
	}

	return req, nil
}

func (s *service) List(
	ctx context.Context,
	subject Subject,
	filter *filtering.QueryFilter,
) (*filtering.QueryFilteredResult[Request], error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	op.Set(subjectIDKey, subject.ID).Set(subjectScopeKey, subject.Scope)

	if err := subject.validate(); err != nil {
		return nil, op.Error(err, "validating dataprivacy subject")
	}

	results, err := s.store.List(ctx, subject, filter)
	if err != nil {
		return nil, op.Error(err, "listing dataprivacy requests")
	}

	return results, nil
}

func (s *service) Confirm(ctx context.Context, requestID string) (*Request, error) {
	req, err := s.transition(ctx, requestID, StatusPending, audit.EventUpdated, map[string]string{"reason": "confirmed"})
	if err != nil {
		return nil, err
	}

	s.confirmedCounter.Add(ctx, 1, requestTypeAttr(req.Type))

	return req, nil
}

func (s *service) Cancel(ctx context.Context, requestID string) (*Request, error) {
	req, err := s.transition(ctx, requestID, StatusCancelled, audit.EventUpdated, map[string]string{"reason": "cancelled by request"})
	if err != nil {
		return nil, err
	}

	s.cancelledCounter.Add(ctx, 1, requestTypeAttr(req.Type))

	return req, nil
}

// transition drives Confirm and Cancel, which differ only in destination.
//
// Both are guarded on StatusAwaitingConfirmation and both run with their audit
// entry in one transaction, so a confirmation that commits without a record of
// who confirmed it is not a state this package can reach.
func (s *service) transition(
	ctx context.Context,
	requestID string,
	to Status,
	event audit.EventType,
	metadata map[string]string,
) (*Request, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	op.Set(requestIDKey, requestID).Set(statusKey, string(to))

	var req *Request

	if err := s.store.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		var err error
		if req, err = s.store.Transition(ctx, q, requestID, []Status{StatusAwaitingConfirmation}, to, s.clock.Now().UTC()); err != nil {
			return err
		}

		return s.record(ctx, q, req, event, metadata)
	}); err != nil {
		// A transition that matched no row means the request was not awaiting
		// confirmation — it was already confirmed, already cancelled, or its
		// window lapsed while the subject was reading the mail. The caller is
		// told which of those it is, not merely that something did not happen.
		if errors.Is(err, ErrRequestNotFound) {
			return nil, op.Error(
				platformerrors.Wrapf(ErrNotAwaitingConfirmation, "dataprivacy request %q", requestID),
				"transitioning dataprivacy request",
			)
		}

		return nil, op.Error(err, "transitioning dataprivacy request")
	}

	return req, nil
}

func (s *service) Download(ctx context.Context, requestID string) (string, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	op.Set(requestIDKey, requestID)

	req, err := s.artifactRequest(ctx, requestID)
	if err != nil {
		return "", op.Error(err, "resolving dataprivacy artifact")
	}

	if s.packager.encrypts() {
		return "", op.Error(
			platformerrors.Wrapf(ErrArtifactEncrypted, "dataprivacy request %q", requestID),
			"signing dataprivacy artifact URL",
		)
	}

	signer, ok := s.uploader.(uploads.URLSigner)
	if !ok {
		return "", op.Error(
			platformerrors.Wrapf(ErrNoURLSigner, "dataprivacy request %q", requestID),
			"signing dataprivacy artifact URL",
		)
	}

	url, err := signer.SignedURL(ctx, req.ArtifactRef, &uploads.SignedURLOptions{
		Method: "GET",
		Expiry: s.cfg.SignedURLTTL,
	})
	if err != nil {
		return "", op.Error(err, "signing dataprivacy artifact URL")
	}

	s.downloadCounter.Add(ctx, 1, requestTypeAttr(req.Type))

	// Recorded, and recorded outside the read's transaction because there is
	// not one. Minting a link to an export is the moment the data becomes
	// reachable, and it is the event an investigation asks about — more so than
	// the export that produced it, which at least required a subject to ask.
	if err = s.recordOutOfBand(ctx, req, audit.EventAccessed, map[string]string{"action": "download_url_issued"}); err != nil {
		op.Acknowledge(err, "recording dataprivacy artifact access")
	}

	return url, nil
}

func (s *service) Open(ctx context.Context, requestID string) (io.ReadCloser, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	op.Set(requestIDKey, requestID)

	req, err := s.artifactRequest(ctx, requestID)
	if err != nil {
		return nil, op.Error(err, "resolving dataprivacy artifact")
	}

	stored, err := uploads.ReadFile(ctx, s.uploader, req.ArtifactRef)
	if err != nil {
		return nil, op.Error(err, "reading dataprivacy artifact")
	}

	decoded, err := s.packager.decode(ctx, stored)
	if err != nil {
		return nil, op.Error(err, "decoding dataprivacy artifact")
	}

	s.downloadCounter.Add(ctx, 1, requestTypeAttr(req.Type))

	if err = s.recordOutOfBand(ctx, req, audit.EventAccessed, map[string]string{"action": "artifact_read"}); err != nil {
		op.Acknowledge(err, "recording dataprivacy artifact access")
	}

	return io.NopCloser(bytes.NewReader(decoded)), nil
}

// artifactRequest resolves a request that actually has a fetchable artifact.
func (s *service) artifactRequest(ctx context.Context, requestID string) (*Request, error) {
	if s.uploader == nil {
		return nil, platformerrors.Wrap(ErrArtifactUnavailable, "no dataprivacy upload manager configured")
	}

	req, err := s.store.Get(ctx, requestID)
	if err != nil {
		return nil, err
	}

	// Status and reference are both checked. A completed export whose reference
	// is empty has been expired and swept; an expired one has too, and saying
	// so by status alone would miss the window between the object's deletion
	// and the row's update.
	if req.Status != StatusCompleted || req.ArtifactRef == "" {
		return nil, platformerrors.Wrapf(
			ErrArtifactUnavailable,
			"dataprivacy request %q is %s", requestID, req.Status,
		)
	}

	return req, nil
}

// record appends an audit entry inside the caller's transaction. It is a no-op
// without a Recorder.
func (s *service) record(
	ctx context.Context,
	q database.SQLQueryExecutor,
	req *Request,
	event audit.EventType,
	metadata map[string]string,
) error {
	if s.recorder == nil {
		return nil
	}

	return s.recorder.Record(ctx, q, s.entryFor(ctx, req, event, metadata))
}

// recordOutOfBand appends an audit entry in a transaction of its own, for the
// reads that have none.
func (s *service) recordOutOfBand(ctx context.Context, req *Request, event audit.EventType, metadata map[string]string) error {
	if s.recorder == nil {
		return nil
	}

	return s.store.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		return s.recorder.Record(ctx, q, s.entryFor(ctx, req, event, metadata))
	})
}

// entryFor renders one audit entry about a request.
//
// The subject's ID is recorded and nothing else about them is. An audit log is
// durable by design, and copying a person's data into the log that records the
// request to export it would defeat both.
func (s *service) entryFor(ctx context.Context, req *Request, event audit.EventType, metadata map[string]string) *audit.Entry {
	fields := map[string]string{
		"request_type": string(req.Type),
		"status":       string(req.Status),
		"subject_id":   req.Subject.ID,
		"subject_type": string(req.Subject.Type),
	}
	maps.Copy(fields, metadata)

	return &audit.Entry{
		EventType:    event,
		ResourceType: auditResourceType,
		ResourceID:   req.ID,
		Actor:        s.actor(ctx),
		Scope:        req.Subject.Scope,
		Metadata:     fields,
		RecordedAt:   s.clock.Now().UTC(),
	}
}

// requestTypeAttr labels a measurement with its request type. There are exactly
// two, so this cannot grow cardinality, and without it the counters cannot
// distinguish a surge of exports from a surge of erasures — which need entirely
// different responses.
func requestTypeAttr(t RequestType) metric.MeasurementOption {
	return metric.WithAttributes(attribute.String(requestTypeKey, string(t)))
}
