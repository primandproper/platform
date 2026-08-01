package dataprivacy

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/primandproper/platform-go/v9/database"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/filtering"
)

// serviceName names the loggers, spans, and metrics this package emits.
const serviceName = "dataprivacy"

// Observability keys for this package's spans and log fields. Declared once so
// a field set on a span and the same field logged beside it cannot drift, and
// so the dataprivacy. prefix is applied uniformly — an un-namespaced attribute
// name collides with every other component writing to the same trace.
//
// Nothing here carries a collected fragment, an artifact's bytes, or anything
// that came back from a Collector. This package exists to move a file
// containing everything an application knows about a person, and a span
// exporter is durable storage that person never consented to.
const (
	requestIDKey    = "dataprivacy.request_id"
	requestTypeKey  = "dataprivacy.request_type"
	subjectIDKey    = "dataprivacy.subject_id"
	subjectTypeKey  = "dataprivacy.subject_type"
	subjectScopeKey = "dataprivacy.subject_scope"
	statusKey       = "dataprivacy.status"
	sectionKey      = "dataprivacy.section"
	sectionCountKey = "dataprivacy.section_count"
	failureCountKey = "dataprivacy.failure_count"
	artifactRefKey  = "dataprivacy.artifact_ref"
	artifactSizeKey = "dataprivacy.artifact_bytes"
	deletedKey      = "dataprivacy.deleted"
	anonymizedKey   = "dataprivacy.anonymized"
	retainedKey     = "dataprivacy.retained"
	claimedKey      = "dataprivacy.claimed"
	expiredKey      = "dataprivacy.expired"
	overdueKey      = "dataprivacy.overdue"
	sweptKey        = "dataprivacy.swept"

	// Store-layer keys. The database client traces the statement, but with the
	// SQL text suppressed by default — so without these a trace shows an
	// anonymous query span and no indication of which request it was about.
	storeOpKey      = "dataprivacy.store_operation"
	fromStatusKey   = "dataprivacy.from_status"
	rowsAffectedKey = "dataprivacy.rows_affected"
	guardMissedKey  = "dataprivacy.guard_missed"
	selectedKey     = "dataprivacy.selected"
	resultCountKey  = "dataprivacy.result_count"
	resultTotalKey  = "dataprivacy.result_total"
	limitKey        = "dataprivacy.limit"
	lapsedKey       = "dataprivacy.lapsed"
	reapedKey       = "dataprivacy.reaped"
	attemptsKey     = "dataprivacy.attempts"
	terminalKey     = "dataprivacy.terminal"
)

var (
	// ErrNilStore indicates a nil Store. It wraps errors.ErrNilInputParameter,
	// so a caller may check either.
	ErrNilStore = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil dataprivacy store")

	// ErrNilDatabaseClient indicates a nil database.Client. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilDatabaseClient = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil database client")

	// ErrNilExecutor indicates a Store method that runs in the caller's
	// transaction was called without one.
	ErrNilExecutor = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil query executor")

	// ErrNilRequest indicates a nil *Request.
	ErrNilRequest = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil dataprivacy request")

	// ErrEmptySubjectID indicates a Subject with no ID. Every request is about
	// somebody, and a request about nobody would fan out over every collector
	// asking for the empty string's data — which some of them will answer.
	ErrEmptySubjectID = platformerrors.New("empty dataprivacy subject ID")

	// ErrUnknownRequestType indicates a RequestType outside the two this package
	// implements.
	ErrUnknownRequestType = platformerrors.New("unknown dataprivacy request type")

	// ErrRequestNotFound indicates a request ID that is not in the table. It may
	// mean the request never existed, or that retention has swept it.
	ErrRequestNotFound = platformerrors.New("dataprivacy request not found")

	// ErrNotAwaitingConfirmation indicates a Confirm or Cancel naming a request
	// that is not waiting for one — because it was never two-phase, because it
	// has already been confirmed, or because its confirmation window lapsed.
	ErrNotAwaitingConfirmation = platformerrors.New("dataprivacy request is not awaiting confirmation")

	// ErrNoCollectors indicates an export Service built with no registered
	// Collector. It is refused at construction rather than at fulfillment: an
	// export service with no collectors produces a valid, empty, and entirely
	// wrong artifact, and a subject who receives one has been told that nothing
	// is held about them.
	ErrNoCollectors = platformerrors.New("no dataprivacy collectors registered")

	// ErrNoErasers indicates an erasure Service built with no registered Eraser,
	// refused for the same reason as ErrNoCollectors and with a worse failure
	// mode: an erasure that erases nothing reports success.
	ErrNoErasers = platformerrors.New("no dataprivacy erasers registered")

	// ErrDuplicateKey indicates two registrations under one key. Keys become
	// section names in the artifact, so a silent overwrite would drop a domain
	// from every export without any signal that it had.
	ErrDuplicateKey = platformerrors.New("duplicate dataprivacy registration key")

	// ErrInvalidKey indicates a registration key that is empty or is not a
	// plain identifier. Keys are JSON object keys in the artifact and path
	// segments in telemetry, so they are restricted rather than escaped.
	ErrInvalidKey = platformerrors.New("invalid dataprivacy registration key")

	// ErrArtifactUnavailable indicates a Download or Open for a request that has
	// no artifact: an erasure, a request that has not completed, or one whose
	// artifact has expired and been deleted.
	ErrArtifactUnavailable = platformerrors.New("dataprivacy artifact is unavailable")

	// ErrArtifactEncrypted indicates a Download against a Service configured
	// with an Encryptor.
	//
	// The two are genuinely incompatible rather than merely awkward. A signed
	// URL hands the client the stored object, and the stored object is
	// ciphertext this package base64s — a subject who follows that link gets a
	// file they cannot open, and finds out thirty days into a statutory window.
	// Encryption at rest and direct-to-bucket delivery are a choice between two
	// things, so configuring both fails here rather than at the subject.
	ErrArtifactEncrypted = platformerrors.New("dataprivacy artifact is encrypted and cannot be delivered by signed URL")

	// ErrNoURLSigner indicates a Download against an UploadManager that cannot
	// sign URLs. Not every provider can — the filesystem one certainly cannot —
	// and Open is the path that works everywhere.
	ErrNoURLSigner = platformerrors.New("dataprivacy upload manager cannot sign URLs")
)

// SubjectType distinguishes the kinds of thing a request can be about.
//
// Like audit.ActorType this is a bare string with suggested constants rather
// than a closed set: an application whose data hangs off a third kind of
// principal should say so rather than misfile it as one of these.
type SubjectType string

const (
	// SubjectUser is a natural person — the subject GDPR and CCPA are written
	// about.
	SubjectUser SubjectType = "user"
	// SubjectAccount is an account, tenant, or organization. An account-scoped
	// request is the one that arrives when a business customer leaves.
	SubjectAccount SubjectType = "account"
)

// Subject is who or what a request is about.
type Subject struct {
	// ID identifies the subject. Required.
	ID string `json:"id"`
	// Scope is the account or tenant the request is confined to, when it is
	// confined at all. Empty means the request spans every scope the subject
	// appears in, which is what a plain "give me my data" asks for.
	//
	// It is one opaque string rather than a typed tenancy path for the same
	// reason audit.Entry.Scope is: tenancy depth is an application's decision,
	// and a two-level model cannot express one level or three.
	Scope string `json:"scope,omitempty"`
	// Type says what kind of subject it is.
	Type SubjectType `json:"type,omitempty"`
}

// validate reports whether the subject names anything.
func (s Subject) validate() error {
	if s.ID == "" {
		return ErrEmptySubjectID
	}

	return nil
}

// RequestType names what a request asks for.
type RequestType string

const (
	// RequestExport is a subject access request: collect everything held about
	// the subject and deliver it.
	RequestExport RequestType = "export"
	// RequestErasure is a right-to-be-forgotten request: delete or anonymize
	// what is held about the subject, retaining only what must be retained.
	RequestErasure RequestType = "erasure"
)

// Valid reports whether t is a request type this package implements.
func (t RequestType) Valid() bool {
	return t == RequestExport || t == RequestErasure
}

// Status is where a request has got to.
//
// The transitions between these are diagrammed in the package overview.
//
// expired is reachable only from completed, and only for an export: it is the
// state in which the artifact has been deleted and the record of the request
// survives. It is the state people forget, and it is the one that decides
// whether a file containing everything you know about a person is a temporary
// artifact or a permanent object in a bucket.
type Status string

const (
	// StatusAwaitingConfirmation is an erasure that has been submitted but not
	// yet confirmed. Reachable only when a confirmation window is configured.
	StatusAwaitingConfirmation Status = "awaiting_confirmation"
	// StatusPending is a request waiting to be claimed.
	StatusPending Status = "pending"
	// StatusProcessing is a request a worker has leased and is fulfilling.
	StatusProcessing Status = "processing"
	// StatusCompleted is a request that was fulfilled. An export in this state
	// has an ArtifactRef; it may also have Failures, which is what a partial
	// export looks like.
	StatusCompleted Status = "completed"
	// StatusFailed is a request that could not be fulfilled at all.
	StatusFailed Status = "failed"
	// StatusExpired is a completed export whose artifact has been deleted.
	StatusExpired Status = "expired"
	// StatusCancelled is an erasure that was never confirmed — withdrawn by the
	// subject, or left to sit until its confirmation window lapsed.
	StatusCancelled Status = "cancelled"
)

// Terminal reports whether a status is one nothing moves out of.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusExpired, StatusCancelled:
		return true
	case StatusAwaitingConfirmation, StatusPending, StatusProcessing:
		return false
	default:
		return false
	}
}

// Request is one export or erasure and everything known about how it went.
type Request struct {
	// RequestedAt is when the request was submitted. It is the instant the
	// statutory clock starts, so it is stamped once and never rewritten — not
	// by a confirmation, and not by a retry.
	RequestedAt time.Time `json:"requestedAt"`

	// DueAt is when the response is legally owed, computed at submission from
	// the configured response window for the request type. See Overdue.
	DueAt time.Time `json:"dueAt"`

	// ExpiresAt is when the artifact is deleted and an export moves to
	// StatusExpired. For an erasure it is when the confirmation window lapses,
	// and it is zero once the erasure is confirmed.
	ExpiresAt time.Time `json:"expiresAt"`

	// CompletedAt is when the request reached a terminal state. Nil until it
	// does.
	CompletedAt *time.Time `json:"completedAt,omitempty"`

	// Failures records the collector or eraser keys that errored, against the
	// rendered error. A completed export with a non-empty Failures is a partial
	// export: the artifact was delivered, and its manifest names these same
	// sections as missing.
	//
	// It is a rendered string rather than an error because it is stored and read
	// by a human — often a regulator — not re-wrapped by a caller.
	Failures map[string]string `json:"failures,omitempty"`

	// Retained records, per eraser key, what an erasure kept and why. It is the
	// answer to "you said you deleted everything", and the reason ErasureOutcome
	// carries a legal basis rather than only a count.
	Retained map[string]string `json:"retained,omitempty"`

	// ID identifies the request.
	ID string `json:"id"`

	// ArtifactRef is the uploads path of the export artifact. Empty for an
	// erasure, for an incomplete export, and for an expired one — the path is
	// cleared when the object is deleted, so a stale reference cannot outlive
	// the thing it referenced.
	ArtifactRef string `json:"artifactRef,omitempty"`

	// LastError is why a failed request failed, rendered. Empty otherwise.
	LastError string `json:"lastError,omitempty"`

	// Subject is who the request is about.
	Subject Subject `json:"subject"`

	// Type is what was asked for.
	Type RequestType `json:"type"`

	// Status is where it got to.
	Status Status `json:"status"`

	// ArtifactBytes is the stored size of the artifact, after compression and
	// encryption. Zero for an erasure or an unfulfilled export.
	ArtifactBytes int64 `json:"artifactBytes,omitempty"`

	// Deleted and Anonymized are the erasure totals summed across every eraser.
	Deleted    int64 `json:"deleted,omitempty"`
	Anonymized int64 `json:"anonymized,omitempty"`

	// Attempts is how many times a worker has claimed this request. It is
	// incremented at claim rather than at failure, so a request that reliably
	// kills its worker eventually fails instead of being reclaimed forever.
	Attempts int `json:"attempts"`
}

// Partial reports whether a completed request left something uncollected or
// unerased. A caller rendering a status page should say so: "completed" over a
// manifest with three missing sections is a misleading thing to show a subject
// who has thirty days to complain about it.
func (r *Request) Partial() bool {
	return r != nil && len(r.Failures) > 0
}

// Overdue reports whether the statutory response window has lapsed with the
// request still unfulfilled. A request that completed after its deadline is not
// overdue — it is late, which is a fact about the past and not a thing to page
// somebody about.
func (r *Request) Overdue(now time.Time) bool {
	if r == nil || r.DueAt.IsZero() || r.Status.Terminal() {
		return false
	}

	return now.After(r.DueAt)
}

// Collector produces one domain's view of a subject.
//
// Collect returns already-encoded JSON rather than a value to be marshaled, and
// that is the load-bearing decision in this package. The prior art this
// generalizes had every domain mutate one shared aggregate struct, so adding a
// domain meant editing a central type that imported every domain package — a
// cost paid on every schema change, by the one file most likely to conflict. A
// library cannot have that type at all, and it turns out not to need one: an
// opaque fragment per key composes into a document without the library knowing
// what any of it means.
//
// Returning nil, nil is how a domain says "nothing about this subject". The
// section is then omitted from the artifact rather than written as null, so an
// export's sections are the domains that actually held something.
//
// A Collector must not return partially-collected data alongside an error. The
// fragment is used or the error is recorded; there is no path that writes both.
//
// There is deliberately no as-of time in this signature, and it is worth being
// clear about what that means. A fragment is the domain's state at the instant
// Collect ran, which is when a Worker got to the request — not when the subject
// asked. The two differ by the queue depth plus any retries, and because
// collectors run concurrently they differ from each other as well: an artifact
// is a smear across the collection window rather than a snapshot at any one
// instant. Manifest.GeneratedAt is the only time the artifact states.
//
// This matches the ordinary reading of a subject access request — the data held
// when the response is produced — and it is the only thing a library can
// promise. Bounding an export to data created on or before Request.RequestedAt
// would have to be a parameter here, honored by every registered Collector, and
// nothing in this package could enforce it: a domain with no reliable creation
// timestamp cannot answer the question at all, and one that ignored the bound
// would be silently wrong in the direction that matters. An application whose
// jurisdiction or dispute posture needs that guarantee has to implement it in
// its collectors, and know that it has.
type Collector interface {
	Collect(ctx context.Context, subject Subject) (json.RawMessage, error)
}

// CollectorFunc adapts a function to Collector.
type CollectorFunc func(ctx context.Context, subject Subject) (json.RawMessage, error)

// Collect implements Collector.
func (f CollectorFunc) Collect(ctx context.Context, subject Subject) (json.RawMessage, error) {
	return f(ctx, subject)
}

// Eraser removes or anonymizes one domain's data about a subject.
//
// It is deliberately separate from Collector rather than derived from it.
// Erasure is not the inverse of export: some data must be retained (financial
// records under tax law, audit entries under legitimate interest) and some must
// be anonymized in place rather than deleted, because a foreign key still
// points at it. Only the domain knows which of the three applies to each of its
// tables, and a library that inferred "erase everything you would have
// exported" would be confidently wrong about all of it.
//
// Erase runs inside the request's transaction and must use the executor it is
// given rather than a handle of its own. Every registered eraser for one
// request shares that transaction, so an erasure is all-or-nothing: a subject
// is not left half-deleted across eleven domains because the ninth timed out.
type Eraser interface {
	Erase(ctx context.Context, q database.SQLQueryExecutor, subject Subject) (ErasureOutcome, error)
}

// EraserFunc adapts a function to Eraser.
type EraserFunc func(ctx context.Context, q database.SQLQueryExecutor, subject Subject) (ErasureOutcome, error)

// Erase implements Eraser.
func (f EraserFunc) Erase(ctx context.Context, q database.SQLQueryExecutor, subject Subject) (ErasureOutcome, error) {
	return f(ctx, q, subject)
}

// ErasureOutcome is what one domain did.
type ErasureOutcome struct {
	// Retained names what was kept and the legal basis for keeping it — the
	// string goes into the request record and, in practice, in front of a
	// regulator. "invoices: financial records, retained 7 years under
	// [statute]" is the shape that answers the question; "some data" is not.
	//
	// It is keyed so one domain can retain several things for different
	// reasons, which is the normal case rather than the exotic one.
	Retained map[string]string `json:"retained,omitempty"`
	// Deleted is how many rows were destroyed.
	Deleted int64 `json:"deleted"`
	// Anonymized is how many rows were kept but stripped of anything
	// identifying. A row that was both is counted once, here.
	Anonymized int64 `json:"anonymized"`
}

// Service is the application-facing seam: submit a request, ask after one, list
// them.
//
// Fulfillment is deliberately not on this interface. A Submit that collected
// eleven domains inline would tie a regulatory obligation to the lifetime of an
// HTTP request, and the one guarantee a subject access request needs is that it
// survives the process that accepted it. Submit writes a row; a Worker
// fulfills it.
type Service interface {
	// Submit records a new request and returns it. An erasure submitted to a
	// Service with a confirmation window returns StatusAwaitingConfirmation and
	// does nothing further until Confirm.
	Submit(ctx context.Context, subject Subject, t RequestType) (*Request, error)

	// Get reads one request. It returns an error wrapping ErrRequestNotFound
	// when there is no such request.
	Get(ctx context.Context, requestID string) (*Request, error)

	// List pages through a subject's requests. A subject is entitled to know
	// what has been asked in their name, which is the reason this is scoped to
	// a subject rather than global.
	//
	// Ordering follows the filter's SortBy — ascending by default, as
	// filtering.DefaultQueryFilter asks. Requests are ordered by ID, which for
	// generated identifiers is submission order.
	List(ctx context.Context, subject Subject, f *filtering.QueryFilter) (*filtering.QueryFilteredResult[Request], error)

	// Confirm moves an erasure out of StatusAwaitingConfirmation and queues it.
	// It returns an error wrapping ErrNotAwaitingConfirmation for a request in
	// any other state, including one whose window has already lapsed.
	Confirm(ctx context.Context, requestID string) (*Request, error)

	// Cancel withdraws an unconfirmed erasure.
	Cancel(ctx context.Context, requestID string) (*Request, error)

	// Download mints a time-limited URL for a completed export's artifact,
	// letting the subject fetch it from storage without the bytes passing
	// through the application. The URL expires; the artifact behind it is
	// deleted at ExpiresAt whether or not anyone fetched it.
	//
	// It returns an error wrapping ErrArtifactUnavailable for a request with no
	// artifact, ErrArtifactEncrypted when the Service encrypts artifacts at
	// rest, and ErrNoURLSigner when the storage provider cannot sign.
	Download(ctx context.Context, requestID string) (string, error)

	// Open streams a completed export's artifact as canonical JSON, reversing
	// whatever compression and encryption it was stored under. The caller must
	// close it.
	//
	// It is the path that always works — every storage provider, encrypted or
	// not — at the cost of proxying the bytes through the application. Prefer
	// Download where it is available, and reach for this when it is not.
	Open(ctx context.Context, requestID string) (io.ReadCloser, error)
}
