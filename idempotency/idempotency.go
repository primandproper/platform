package idempotency

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v8/cache"
	"github.com/primandproper/platform-go/v8/clock"
	"github.com/primandproper/platform-go/v8/distributedlock"
	platformerrors "github.com/primandproper/platform-go/v8/errors"
	"github.com/primandproper/platform-go/v8/identifiers"
	"github.com/primandproper/platform-go/v8/observability"
	"github.com/primandproper/platform-go/v8/observability/logging"
	"github.com/primandproper/platform-go/v8/observability/metrics"
	"github.com/primandproper/platform-go/v8/observability/tracing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// DefaultTTL is how long a completed record is replayable for.
	DefaultTTL = 24 * time.Hour
	// DefaultInFlightTTL bounds how long a claim survives without being
	// completed. It must exceed the worst-case duration of the work being
	// guarded — see the package documentation on choosing it.
	DefaultInFlightTTL = 2 * time.Minute
	// DefaultMaxKeyLength is the longest key accepted, matching the limit
	// Stripe publishes for the same header.
	DefaultMaxKeyLength = 255
	// DefaultKeyPrefix namespaces both the store and lock keys, so an
	// idempotency key cannot collide with an unrelated entry in a cache or
	// locker shared with something else.
	DefaultKeyPrefix = "idempotency:"

	// serviceName names the loggers, spans, and metrics this package emits.
	serviceName = "idempotency"

	// recordVersion stamps every record written. A deploy that changes the
	// shape of T (or of Record itself) bumps it, and records written by the
	// previous shape are then ignored rather than misread. Without it a
	// gob-decoded record from an older binary would surface as a plausible
	// but wrong replay.
	recordVersion = 1
)

// Sentinels. errors/http and errors/grpc map these onto status codes, so those
// packages import this one. That direction is load-bearing: nothing here may
// import errors/http or errors/grpc, or the cycle closes. It is also why the
// transport adapters live in their own packages rather than here.
var (
	// ErrInFlight indicates the key names work that is currently running
	// elsewhere. The caller has no way to know whether it will succeed, so the
	// only safe answer is to refuse and let the client retry later.
	ErrInFlight = platformerrors.New("idempotency key is in flight")
	// ErrFingerprintMismatch indicates the key was already used for a
	// different request. Replaying the stored result would hide a client bug,
	// so the reuse is reported instead.
	ErrFingerprintMismatch = platformerrors.New("idempotency key reused with a different request")
	// ErrKeyRequired indicates an empty key was supplied.
	ErrKeyRequired = platformerrors.New("empty idempotency key")
	// ErrKeyTooLong indicates a key longer than the configured maximum.
	ErrKeyTooLong = platformerrors.New("idempotency key exceeds the maximum length")
	// ErrKeyInvalid indicates a key containing bytes outside printable ASCII.
	ErrKeyInvalid = platformerrors.New("idempotency key contains disallowed characters")
	// ErrStoreUnavailable indicates the record store could not be reached and
	// the manager is configured to fail closed. Running the work anyway could
	// repeat an effect that already happened.
	ErrStoreUnavailable = platformerrors.New("idempotency store unavailable")
	// ErrEmptyFingerprint indicates Do was called without a fingerprint. An
	// empty one would make every request for a key look identical and disable
	// mismatch detection entirely, so it is rejected rather than defaulted.
	ErrEmptyFingerprint = platformerrors.New("empty idempotency fingerprint")
	// ErrNilStore indicates NewManager was called without a record store. It
	// wraps errors.ErrNilInputParameter, so a caller may check either.
	ErrNilStore = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil idempotency store")
	// ErrNilLocker indicates NewManager was called without a locker. It has no
	// default: an implicit noop would silently remove mutual exclusion. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilLocker = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil idempotency locker")
	// ErrNilFunc indicates Do was called with no work to run. It wraps
	// errors.ErrNilInputParameter, so a caller may check either.
	ErrNilFunc = platformerrors.Wrap(platformerrors.ErrNilInputParameter, "nil idempotency func")
	// ErrInvalidTTL indicates a non-positive TTL was configured.
	ErrInvalidTTL = platformerrors.New("invalid idempotency TTL")
	// ErrRecordableTypeMismatch indicates WithRecordable was given a predicate
	// for a type other than the Manager's. Option carries no type parameter, so
	// the compiler cannot catch this; NewManager reports it instead.
	ErrRecordableTypeMismatch = platformerrors.New("recordable predicate type does not match manager type")
)

// State is the lifecycle stage of a record.
type State uint8

const (
	// StateInFlight marks a claim: work has started and has not reported back.
	StateInFlight State = iota + 1
	// StateCompleted marks a finished result that is safe to replay.
	StateCompleted
)

// StoreFailurePolicy decides what happens when the record store cannot be
// read. It is the most consequential setting in this package, because the two
// answers fail in opposite directions.
type StoreFailurePolicy uint8

const (
	// FailClosed refuses the request when the store is unreachable. The
	// default, and the right answer whenever the guarded work costs money: a
	// brief outage becomes downtime rather than duplicate charges.
	FailClosed StoreFailurePolicy = iota
	// FailOpen runs the work anyway, trading the guarantee for availability.
	// Appropriate only where a duplicate effect is cheaper than a rejection.
	FailOpen
)

type (
	// Record is what the store holds for a key. It is written twice per
	// execution: once to claim the key, once to record the outcome.
	//
	// T must be a concrete struct with exported fields — see the package
	// documentation on what the store can round-trip.
	Record[T any] struct {
		// CreatedAt is when this revision of the record was written.
		CreatedAt time.Time
		// Value is the recorded result, set only once State is
		// StateCompleted.
		Value *T
		// Fingerprint identifies the request this key was used for, so a
		// second, different request under the same key can be detected.
		Fingerprint Fingerprint
		// ClaimID identifies the execution that owns the claim. Only its owner
		// may complete or release it, which is what stops an execution that
		// outlived its claim from overwriting whoever re-claimed the key.
		ClaimID string
		// Version is the record shape this was written with.
		Version int
		// State is the lifecycle stage.
		State State
	}

	// Result is the outcome of Do.
	Result[T any] struct {
		// Value is the result of the work, whether it just ran or was
		// replayed.
		Value *T
		// Replayed reports whether Value came from a stored record rather
		// than from running the work.
		Replayed bool
	}

	// Manager runs work at most once per key.
	//
	// It is a concrete type rather than an interface: there is one
	// implementation, and the seams worth swapping — the store and the locker
	// — are already interfaces with their own mocks.
	Manager[T any] struct {
		store  cache.Cache[Record[T]]
		locker distributedlock.ScopedLocker
		o11y   observability.Observer
		logger logging.Logger
		clock  clock.Clock

		recordable func(*T) bool

		requestCounter       metrics.Int64Counter
		claimLostCounter     metrics.Int64Counter
		recordFailureCounter metrics.Int64Counter
		storeErrorCounter    metrics.Int64Counter
		staleRecordCounter   metrics.Int64Counter
		latencyHist          metrics.Float64Histogram

		tracerProvider  tracing.TracerProvider
		metricsProvider metrics.Provider

		keyPrefix          string
		ttl                time.Duration
		inFlightTTL        time.Duration
		maxKeyLength       int
		storeFailurePolicy StoreFailurePolicy
	}

	// Option configures a Manager at construction.
	//
	// It is deliberately not parameterized on the Manager's T. None of these
	// settings depend on it, and Go cannot infer a type argument from a call's
	// result type — so an Option would force every call site to spell the
	// Manager's type out by hand — WithTTL[Receipt](time.Hour) — forever.
	//
	// WithRecordable is the one setting that does depend on T. It stays generic
	// but still needs no annotation, because T is inferable from the predicate
	// it is handed; see its documentation for how a mismatch is reported.
	Option func(*managerOptions)

	// managerOptions accumulates what the options set, so that Option can stay
	// free of the Manager's type parameter.
	managerOptions struct {
		clock           clock.Clock
		logger          logging.Logger
		tracerProvider  tracing.TracerProvider
		metricsProvider metrics.Provider

		// recordable holds a func(*T) bool for the T of the Manager being
		// built. It is typed as any because Option cannot name T; NewManager
		// asserts it back to the concrete signature and reports a mismatch
		// rather than ignoring it.
		recordable any

		keyPrefix          *string
		ttl                time.Duration
		inFlightTTL        time.Duration
		maxKeyLength       int
		storeFailurePolicy StoreFailurePolicy
	}
)

// WithTTL sets how long a completed record stays replayable.
func WithTTL(ttl time.Duration) Option {
	return func(o *managerOptions) {
		if ttl > 0 {
			o.ttl = ttl
		}
	}
}

// WithInFlightTTL bounds how long a claim survives without completing.
//
// It is the deadline for the guarded work, not a performance knob. Set it
// below the work's worst case and a slow execution loses its claim while still
// running, which is the one path that can still produce a duplicate effect —
// watch idempotency_claims_lost.
func WithInFlightTTL(ttl time.Duration) Option {
	return func(o *managerOptions) {
		if ttl > 0 {
			o.inFlightTTL = ttl
		}
	}
}

// WithMaxKeyLength overrides the longest accepted key.
func WithMaxKeyLength(maxLength int) Option {
	return func(o *managerOptions) {
		if maxLength > 0 {
			o.maxKeyLength = maxLength
		}
	}
}

// WithKeyPrefix overrides the namespace applied to store and lock keys.
//
// An empty prefix is honored rather than ignored, so a caller can deliberately
// opt out of namespacing; that is why this is the one setting held as a pointer.
func WithKeyPrefix(prefix string) Option {
	return func(o *managerOptions) {
		o.keyPrefix = &prefix
	}
}

// WithRecordable sets the predicate deciding whether a result is worth
// recording. A result it rejects releases the claim instead, so the next
// attempt runs the work again.
//
// This is how a caller expresses "that failure was ours, not theirs": a
// server-side error usually means the effect did not land, and pinning it for
// the whole TTL would strand a client that could have succeeded on retry.
//
// T is inferred from the predicate, so this needs no type argument:
//
//	idempotency.WithRecordable(func(r *Receipt) bool { return r.Charged })
//
// It must match the Manager it configures. Because Option carries no type
// parameter, a predicate for the wrong type cannot be rejected by the compiler;
// NewManager returns ErrRecordableTypeMismatch instead, at construction, before
// any work runs through it.
func WithRecordable[T any](recordable func(*T) bool) Option {
	return func(o *managerOptions) {
		if recordable != nil {
			o.recordable = recordable
		}
	}
}

// WithStoreFailurePolicy chooses what happens when the store cannot be read.
func WithStoreFailurePolicy(policy StoreFailurePolicy) Option {
	return func(o *managerOptions) {
		o.storeFailurePolicy = policy
	}
}

// WithLogger attaches a logger.
func WithLogger(logger logging.Logger) Option {
	return func(o *managerOptions) {
		o.logger = logger
	}
}

// WithTracerProvider attaches a tracer provider.
func WithTracerProvider(tracerProvider tracing.TracerProvider) Option {
	return func(o *managerOptions) {
		o.tracerProvider = tracerProvider
	}
}

// WithMetricsProvider attaches a metrics provider.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *managerOptions) {
		o.metricsProvider = metricsProvider
	}
}

// WithClock swaps the clock used to stamp records.
func WithClock(c clock.Clock) Option {
	return func(o *managerOptions) {
		if c != nil {
			o.clock = c
		}
	}
}

// NewManager builds a Manager over a record store and a locker.
//
// The locker is required and has no default. An implicit noop would leave
// replay working while quietly removing mutual exclusion, which is the failure
// mode hardest to notice and most expensive to meet.
func NewManager[T any](
	store cache.Cache[Record[T]],
	locker distributedlock.ScopedLocker,
	opts ...Option,
) (*Manager[T], error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if locker == nil {
		return nil, ErrNilLocker
	}

	o := &managerOptions{
		clock:        clock.NewClock(),
		ttl:          DefaultTTL,
		inFlightTTL:  DefaultInFlightTTL,
		maxKeyLength: DefaultMaxKeyLength,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	m := &Manager[T]{
		store:              store,
		locker:             locker,
		clock:              o.clock,
		logger:             o.logger,
		tracerProvider:     o.tracerProvider,
		metricsProvider:    o.metricsProvider,
		keyPrefix:          DefaultKeyPrefix,
		ttl:                o.ttl,
		inFlightTTL:        o.inFlightTTL,
		maxKeyLength:       o.maxKeyLength,
		storeFailurePolicy: o.storeFailurePolicy,
		recordable:         func(*T) bool { return true },
	}

	if o.keyPrefix != nil {
		m.keyPrefix = *o.keyPrefix
	}

	// Asserted rather than assumed: Option cannot name T, so this is where a
	// predicate built for another type is caught. Failing here means it is
	// caught at construction, before a single request has run through the
	// Manager believing its results were being filtered.
	if o.recordable != nil {
		recordable, ok := o.recordable.(func(*T) bool)
		if !ok {
			return nil, platformerrors.Wrapf(
				ErrRecordableTypeMismatch, "predicate is %T, want func(*%T) bool", o.recordable, *new(T),
			)
		}

		m.recordable = recordable
	}

	if m.ttl <= 0 || m.inFlightTTL <= 0 {
		return nil, ErrInvalidTTL
	}

	m.o11y = observability.NewObserver(serviceName, m.logger, m.tracerProvider)
	m.logger = m.o11y.Logger()

	mp := metrics.EnsureMetricsProvider(m.metricsProvider)

	var err error
	if m.requestCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_requests", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating requests counter")
	}
	if m.claimLostCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_claims_lost", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating claims lost counter")
	}
	if m.recordFailureCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_record_failures", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating record failures counter")
	}
	if m.storeErrorCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_store_errors", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating store errors counter")
	}
	if m.staleRecordCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_stale_records", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating stale records counter")
	}
	if m.latencyHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", serviceName)); err != nil {
		return nil, platformerrors.Wrap(err, "creating latency histogram")
	}

	return m, nil
}

// DoOption overrides a Manager-level setting for one call.
//
// The Manager's own settings are the defaults for every call through it; these
// exist so that one Manager can serve endpoints whose requirements differ,
// rather than forcing a second Manager per variation.
//
// Like Option, it carries no type parameter: nothing here depends on the
// Manager's T, and one would only force it onto every call site.
type DoOption func(*doOptions)

// doOptions holds the per-call overrides. A nil field means "inherit from the
// Manager", which is what keeps an option's absence distinguishable from an
// option set to a zero value.
type doOptions struct {
	ttl *time.Duration
}

// newDoOptions applies opts, ignoring nil entries.
func newDoOptions(opts []DoOption) *doOptions {
	o := &doOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithCallTTL overrides how long this call's completed record is retained.
//
// Retention is the window in which a retry replays instead of re-running, so it
// belongs to the operation rather than to the Manager: a payment worth
// protecting for a day and a profile update worth protecting for a minute can
// then share one Manager. A non-positive value inherits the Manager's TTL.
func WithCallTTL(ttl time.Duration) DoOption {
	return func(o *doOptions) {
		if ttl > 0 {
			o.ttl = &ttl
		}
	}
}

// Do runs fn at most once for key.
//
// The fingerprint identifies the request the key is being used for. A stored
// record whose fingerprint differs yields ErrFingerprintMismatch rather than a
// replay, which is what stops one key from silently answering two different
// requests.
//
// fn runs outside the lock. Only the claim is serialized, so the lock is held
// for two store round trips regardless of how long the work takes — see the
// package documentation on why that matters.
//
// An error from fn is returned as-is and nothing is recorded, so the next
// attempt runs the work again. A panic does the same and keeps unwinding.
func (m *Manager[T]) Do(
	ctx context.Context,
	key Key,
	fingerprint Fingerprint,
	fn func(ctx context.Context) (*T, error),
	opts ...DoOption,
) (*Result[T], error) {
	ctx, op := m.o11y.Begin(ctx)
	defer op.End()

	startTime := time.Now()
	defer func() {
		m.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	o := newDoOptions(opts)

	ttl := m.ttl
	if o.ttl != nil {
		ttl = *o.ttl
	}

	if err := ValidateKey(key, m.maxKeyLength); err != nil {
		return nil, op.Error(err, "validating idempotency key")
	}
	if fingerprint == "" {
		return nil, op.Error(ErrEmptyFingerprint, "checking idempotency fingerprint")
	}
	if fn == nil {
		return nil, op.Error(ErrNilFunc, "checking idempotency func")
	}

	// Converted rather than passed through: the span attacher's type switch
	// matches string exactly, so a named string type would miss it.
	op.Set(keyKey, string(key)).Set(fingerprintKey, string(fingerprint))

	storeKey := m.storeKey(key)

	// Read before locking. The overwhelmingly common case is a replay of a
	// completed record, and it costs one round trip with no coordination.
	record, found, err := m.load(ctx, op, storeKey)
	if err != nil {
		return nil, op.Error(err, "loading idempotency record")
	}
	if found {
		return m.replay(ctx, op, record, fingerprint)
	}

	claimID, existing, err := m.claim(ctx, op, key, storeKey, fingerprint)
	if err != nil {
		return nil, op.Error(err, "claiming idempotency key")
	}
	if existing != nil {
		// Someone landed a record between the read above and the lock.
		return m.replay(ctx, op, existing, fingerprint)
	}

	value, err := m.invoke(ctx, op, storeKey, claimID, fn)
	if err != nil {
		m.release(ctx, op, storeKey, claimID)

		return nil, err
	}

	if !m.recordable(value) {
		op.Set(recordedKey, false)
		m.release(ctx, op, storeKey, claimID)
		m.count(ctx, outcomeExecuted)

		return &Result[T]{Value: value}, nil
	}

	m.commit(ctx, op, storeKey, claimID, fingerprint, value, ttl)
	m.count(ctx, outcomeExecuted)

	return &Result[T]{Value: value}, nil
}

// replay turns a stored record into an answer: a mismatched fingerprint is
// reported, a completed record is returned, and a claim someone else holds is
// refused.
//
// The fingerprint is checked before the state, deliberately. A client reusing
// one key for two different requests has a bug worth surfacing immediately;
// answering ErrInFlight instead would tell it to retry, which is the one thing
// that cannot help.
func (m *Manager[T]) replay(
	ctx context.Context,
	op observability.Operation,
	record *Record[T],
	fingerprint Fingerprint,
) (*Result[T], error) {
	if record.Fingerprint != fingerprint {
		m.count(ctx, outcomeMismatch)

		return nil, op.Error(ErrFingerprintMismatch, "matching idempotency fingerprint")
	}

	switch record.State {
	case StateCompleted:
		op.Set(replayedKey, true)
		m.count(ctx, outcomeReplayed)

		return &Result[T]{Value: record.Value, Replayed: true}, nil
	case StateInFlight:
		m.count(ctx, outcomeInFlight)

		return nil, op.Error(ErrInFlight, "checking idempotency claim")
	default:
		// A state this binary does not know is treated the same as a shape it
		// cannot read: refuse rather than guess, since the alternative is
		// running work that may already have run.
		m.staleRecordCounter.Add(ctx, 1)

		return nil, op.Error(ErrInFlight, "reading idempotency record state")
	}
}

// claim writes the in-flight record under the lock, returning either the
// ClaimID it took or the record that made claiming unnecessary.
//
// The lock covers a re-read and a write and nothing else. The re-read is what
// makes it correct: two callers that both missed the pre-lock read would
// otherwise both claim, and the second would overwrite the first.
func (m *Manager[T]) claim(
	ctx context.Context,
	op observability.Operation,
	key Key,
	storeKey string,
	fingerprint Fingerprint,
) (claimID string, existing *Record[T], err error) {
	lockErr := m.locker.WithLock(ctx, m.lockKey(key), func(ctx context.Context) error {
		record, found, loadErr := m.load(ctx, op, storeKey)
		if loadErr != nil {
			return loadErr
		}
		if found {
			existing = record

			return nil
		}

		id := identifiers.New()
		if setErr := m.store.Set(ctx, storeKey, &Record[T]{
			CreatedAt:   m.clock.Now().UTC(),
			Fingerprint: fingerprint,
			ClaimID:     id,
			Version:     recordVersion,
			State:       StateInFlight,
		}, cache.WithExpiry(m.inFlightTTL)); setErr != nil {
			return setErr
		}

		claimID = id

		return nil
	})
	if lockErr != nil {
		return "", nil, lockErr
	}

	return claimID, existing, nil
}

// invoke runs fn, releasing the claim if fn panics.
//
// The deferred release deliberately does not recover: a panic belongs to
// whatever recovery the caller has installed, and swallowing it here would
// turn a crash into a silent wrong answer. All this does is make sure the
// claim does not outlive the work on the way past.
func (m *Manager[T]) invoke(
	ctx context.Context,
	op observability.Operation,
	storeKey, claimID string,
	fn func(ctx context.Context) (*T, error),
) (value *T, err error) {
	returned := false
	defer func() {
		if !returned {
			m.release(ctx, op, storeKey, claimID)
		}
	}()

	value, err = fn(ctx)
	returned = true

	return value, err
}

// commit records a finished result, but only if the claim is still ours.
//
// A failure here is counted and logged, never returned: the work already
// happened and the caller is entitled to its result. What the caller loses is
// the replay, so the next attempt runs the work again — which is exactly what
// idempotency_record_failures is for.
func (m *Manager[T]) commit(
	ctx context.Context,
	op observability.Operation,
	storeKey, claimID string,
	fingerprint Fingerprint,
	value *T,
	ttl time.Duration,
) {
	if !m.stillOurs(ctx, op, storeKey, claimID, "completing") {
		return
	}

	if err := m.store.Set(ctx, storeKey, &Record[T]{
		CreatedAt:   m.clock.Now().UTC(),
		Value:       value,
		Fingerprint: fingerprint,
		ClaimID:     claimID,
		Version:     recordVersion,
		State:       StateCompleted,
	}, cache.WithExpiry(ttl)); err != nil {
		m.recordFailureCounter.Add(ctx, 1)
		op.Acknowledge(err, "recording idempotency result")

		return
	}

	op.Set(recordedKey, true)
}

// release drops our claim so the next attempt can run the work again.
//
// Best-effort by design: if it fails, the claim simply expires on its own
// InFlightTTL and callers see ErrInFlight until then. Surfacing the failure
// would replace a delay with an error for work that already completed.
func (m *Manager[T]) release(ctx context.Context, op observability.Operation, storeKey, claimID string) {
	if !m.stillOurs(ctx, op, storeKey, claimID, "releasing") {
		return
	}

	if err := m.store.Delete(ctx, storeKey); err != nil {
		op.Acknowledge(err, "releasing idempotency claim")
	}
}

// stillOurs reports whether the stored record is the claim this execution took.
//
// It is false when the work outran InFlightTTL and someone else re-claimed the
// key — the one remaining path to a duplicate effect, and the reason
// idempotency_claims_lost is the counter to alert on. Writing through it would
// compound the problem by handing the new owner our result.
func (m *Manager[T]) stillOurs(
	ctx context.Context,
	op observability.Operation,
	storeKey, claimID, action string,
) bool {
	record, found, err := m.load(ctx, op, storeKey)
	if err != nil {
		m.recordFailureCounter.Add(ctx, 1)
		op.Acknowledge(err, "reading idempotency claim before %s", action)

		return false
	}

	if !found || record.ClaimID != claimID {
		m.claimLostCounter.Add(ctx, 1)
		op.Logger().WithValues(map[string]any{
			claimIDKey: claimID,
			actionKey:  action,
		}).Error("idempotency claim lost before it could be completed; the work may run again", ErrInFlight)

		return false
	}

	return true
}

// load reads a record, reporting whether one usable to this binary was found.
//
// A record written by a different shape of this package reads as absent rather
// than as an error: with a day-long TTL, failing on it would turn one bad
// deploy into a day of failures. A decode failure is indistinguishable from a
// transport failure through the cache interface, so it goes through the store
// failure policy instead.
func (m *Manager[T]) load(
	ctx context.Context,
	op observability.Operation,
	storeKey string,
) (record *Record[T], found bool, err error) {
	record, err = m.store.Get(ctx, storeKey)
	switch {
	case err == nil:
	case stderrors.Is(err, cache.ErrNotFound):
		return nil, false, nil
	default:
		m.storeErrorCounter.Add(ctx, 1)

		if m.storeFailurePolicy == FailOpen {
			op.Acknowledge(err, "reading idempotency record, failing open")

			return nil, false, nil
		}

		return nil, false, platformerrors.Wrap(
			platformerrors.Wrap(ErrStoreUnavailable, err.Error()),
			"reading idempotency record",
		)
	}

	if record == nil {
		return nil, false, nil
	}

	if record.Version != recordVersion {
		m.staleRecordCounter.Add(ctx, 1)
		op.Logger().
			WithValue(recordVersionKey, record.Version).
			Debug("ignoring idempotency record written by a different record version")

		return nil, false, nil
	}

	return record, true, nil
}

// count records one resolved request against its outcome.
func (m *Manager[T]) count(ctx context.Context, outcome string) {
	m.requestCounter.Add(ctx, 1, metric.WithAttributes(attribute.String(outcomeKey, outcome)))
}

// storeKey namespaces a caller's key for the record store.
func (m *Manager[T]) storeKey(key Key) string {
	return m.keyPrefix + string(key)
}

// lockKey namespaces a caller's key for the locker. It is deliberately
// distinct from the store key: the two live in different systems, and a shared
// spelling invites the assumption that one can be derived from the other.
func (m *Manager[T]) lockKey(key Key) string {
	return m.keyPrefix + "lock:" + string(key)
}
