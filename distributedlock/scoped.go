package distributedlock

import (
	"context"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/primandproper/platform-go/v7/clock"
	platformerrors "github.com/primandproper/platform-go/v7/errors"
	"github.com/primandproper/platform-go/v7/observability"
	"github.com/primandproper/platform-go/v7/observability/keys"
	"github.com/primandproper/platform-go/v7/observability/logging"
	"github.com/primandproper/platform-go/v7/observability/metrics"
	"github.com/primandproper/platform-go/v7/observability/tracing"
)

const (
	// DefaultScopedLockTTL is the TTL the generic scoped adapter passes to
	// Acquire when WithScopedLockTTL is not supplied.
	DefaultScopedLockTTL = 30 * time.Second
	// DefaultScopedPollInterval is how often the generic scoped adapter re-tries
	// a contended WithLock acquisition when WithScopedPollInterval is not
	// supplied.
	DefaultScopedPollInterval = 100 * time.Millisecond

	// scopedServiceName names the adapter's spans, logger, and metrics. It is
	// deliberately provider-agnostic: a dashboard built on scoped_lock_* works
	// whether the wrapped Locker is redis or memory.
	scopedServiceName = "scoped_lock"
)

// ScopedLocker runs a function while holding a named lock, releasing the lock
// when the function returns — including on panic. It is the surface most lock
// consumers actually want (singleton chores, janitor election, migration
// serialization): there is no handle to carry, no TTL bookkeeping, and no way
// to forget Release.
//
// Obtain one natively from a provider that supports scoped execution (the
// postgres provider's transaction-scoped implementation), or wrap any Locker
// with NewScopedLocker.
type ScopedLocker interface {
	// WithLock blocks until the lock named key is acquired (or ctx is done),
	// runs fn while holding it, and releases on return. fn's error is
	// returned to the caller.
	WithLock(ctx context.Context, key string, fn func(ctx context.Context) error) error
	// TryWithLock never waits: if the lock is currently held elsewhere it
	// returns (false, nil) without running fn. Otherwise it runs fn under the
	// lock and returns (true, fn's error). An acquisition-infrastructure
	// failure returns (false, err).
	TryWithLock(ctx context.Context, key string, fn func(ctx context.Context) error) (bool, error)
}

// ScopedOption configures the generic scoped adapter returned by
// NewScopedLocker.
type ScopedOption func(*scopedLocker)

// WithScopedLockTTL sets the TTL the adapter passes to Acquire. The TTL must
// comfortably exceed fn's worst-case duration: if the underlying lock expires
// while fn is still running, mutual exclusion is no longer guaranteed, and the
// implicit release will surface ErrLockNotHeld in the returned error — and
// increment the scoped_lock_release_failures counter, which is the signal to
// alert on.
func WithScopedLockTTL(ttl time.Duration) ScopedOption {
	return func(s *scopedLocker) {
		s.ttl = ttl
	}
}

// WithScopedPollInterval sets how often WithLock re-tries a contended
// acquisition. TryWithLock never polls.
func WithScopedPollInterval(interval time.Duration) ScopedOption {
	return func(s *scopedLocker) {
		s.pollInterval = interval
	}
}

// WithScopedClock swaps the clock used for contention polling; tests pass a
// fake so WithLock's waiting is deterministic.
func WithScopedClock(c clock.Clock) ScopedOption {
	return func(s *scopedLocker) {
		if c != nil {
			s.clock = c
		}
	}
}

// scopedLocker adapts a Locker into a ScopedLocker: acquire, run, release.
type scopedLocker struct {
	o11y            observability.Observer
	locker          Locker
	clock           clock.Clock
	acquireCounter  metrics.Int64Counter
	contendCounter  metrics.Int64Counter
	errCounter      metrics.Int64Counter
	releaseFailures metrics.Int64Counter
	latencyHist     metrics.Float64Histogram
	waitHist        metrics.Float64Histogram
	ttl             time.Duration
	pollInterval    time.Duration
}

var _ ScopedLocker = (*scopedLocker)(nil)

// NewScopedLocker wraps any Locker in scoped execution. WithLock waits for a
// contended lock by polling Acquire (the Locker atom deliberately has no
// queueing of its own); providers with native waiting (postgres) ship their
// own ScopedLocker and don't need this adapter.
//
// It takes the standard observability triple so that the scoped surface emits
// the same telemetry whichever Locker backs it: the wrapped Locker's own
// Acquire/Release instrumentation describes individual attempts, while
// scoped_lock_* describes the whole acquire-run-release operation, including
// fn's duration and the time spent waiting.
func NewScopedLocker(
	locker Locker,
	logger logging.Logger,
	tracerProvider tracing.TracerProvider,
	metricsProvider metrics.Provider,
	opts ...ScopedOption,
) (ScopedLocker, error) {
	if locker == nil {
		return nil, platformerrors.New("nil locker provided")
	}

	mp := metrics.EnsureMetricsProvider(metricsProvider)

	acquireCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_acquires", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating acquire counter")
	}
	contendCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_contended", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating contention counter")
	}
	errCounter, err := mp.NewInt64Counter(fmt.Sprintf("%s_errors", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating error counter")
	}
	releaseFailures, err := mp.NewInt64Counter(fmt.Sprintf("%s_release_failures", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating release failure counter")
	}
	latencyHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating latency histogram")
	}
	waitHist, err := mp.NewFloat64Histogram(fmt.Sprintf("%s_wait_ms", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating wait histogram")
	}

	s := &scopedLocker{
		o11y:            observability.NewObserver(scopedServiceName, logger, tracerProvider),
		locker:          locker,
		clock:           clock.NewClock(),
		acquireCounter:  acquireCounter,
		contendCounter:  contendCounter,
		errCounter:      errCounter,
		releaseFailures: releaseFailures,
		latencyHist:     latencyHist,
		waitHist:        waitHist,
		ttl:             DefaultScopedLockTTL,
		pollInterval:    DefaultScopedPollInterval,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	return s, nil
}

// WithLock implements ScopedLocker, waiting for a contended lock by polling.
func (s *scopedLocker) WithLock(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()
	op.Set(keys.LockKeyKey, key).Set(keys.LockTTLKey, s.ttl)

	startTime := time.Now()
	defer func() {
		s.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	// polls counts contended attempts. Contention is counted once per call
	// rather than once per poll, so scoped_lock_contended means the same thing
	// here as it does for the natively-waiting postgres implementation; the
	// depth of the wait is carried by scoped_lock_wait_ms and the span's
	// lock.polls attribute instead.
	var polls int
	for {
		held, err := s.locker.Acquire(ctx, key, s.ttl)
		if err == nil {
			if polls > 0 {
				s.contendCounter.Add(ctx, 1)
				op.SpanOnly("lock.polls", polls)
			}
			s.waitHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
			s.acquireCounter.Add(ctx, 1)

			return s.run(ctx, op, held, fn)
		}
		if !stderrors.Is(err, ErrLockNotAcquired) {
			s.errCounter.Add(ctx, 1)

			return op.Error(err, "acquiring scoped lock")
		}

		polls++

		if sleepErr := s.clock.Sleep(ctx, s.pollInterval); sleepErr != nil {
			// Giving up still counts as contention — the caller waited and lost
			// — but a canceled or expired context is the caller's deadline
			// arriving, not an infrastructure failure, so it is traced without
			// incrementing the error counter.
			s.contendCounter.Add(ctx, 1)
			op.SpanOnly("lock.polls", polls)

			return op.Error(sleepErr, "waiting for scoped lock")
		}
	}
}

// TryWithLock implements ScopedLocker.
func (s *scopedLocker) TryWithLock(ctx context.Context, key string, fn func(ctx context.Context) error) (bool, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()
	op.Set(keys.LockKeyKey, key).Set(keys.LockTTLKey, s.ttl)

	startTime := time.Now()
	defer func() {
		s.latencyHist.Record(ctx, float64(time.Since(startTime).Milliseconds()))
	}()

	held, err := s.locker.Acquire(ctx, key, s.ttl)
	if stderrors.Is(err, ErrLockNotAcquired) {
		s.contendCounter.Add(ctx, 1)

		return false, nil
	}
	if err != nil {
		s.errCounter.Add(ctx, 1)

		return false, op.Error(err, "trying scoped lock")
	}

	s.acquireCounter.Add(ctx, 1)

	return true, s.run(ctx, op, held, fn)
}

// run executes fn and releases held afterward, panics included. The release
// uses a non-cancelable context so a canceled caller can't strand the lock
// until TTL expiry.
//
// A release failure is the package's most consequential event: ErrLockNotHeld
// here means the TTL elapsed while fn was still running, so mutual exclusion
// was not actually held for fn's full duration. It is logged at error level,
// attached to the span, and counted separately from acquisition errors rather
// than only being folded into the returned error, which a caller may never
// inspect.
func (s *scopedLocker) run(ctx context.Context, op observability.Operation, held Lock, fn func(ctx context.Context) error) (err error) {
	defer func() {
		releaseErr := held.Release(context.WithoutCancel(ctx))
		if releaseErr == nil {
			return
		}

		s.releaseFailures.Add(ctx, 1)
		if stderrors.Is(releaseErr, ErrLockNotHeld) {
			op.Acknowledge(releaseErr, "scoped lock expired before fn returned: mutual exclusion was not held for the full call")
		} else {
			op.Acknowledge(releaseErr, "releasing scoped lock")
		}

		err = platformerrors.Join(err, platformerrors.Wrap(releaseErr, "releasing scoped lock"))
	}()

	return fn(ctx)
}
