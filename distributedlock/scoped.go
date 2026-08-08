package distributedlock

import (
	"context"
	stderrors "errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/primandproper/platform-go/v9/clock"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/observability"
	"github.com/primandproper/platform-go/v9/observability/keys"
	"github.com/primandproper/platform-go/v9/observability/logging"
	"github.com/primandproper/platform-go/v9/observability/metrics"
	"github.com/primandproper/platform-go/v9/observability/tracing"
)

const (
	// DefaultScopedLockTTL is the TTL the generic scoped adapter passes to
	// Acquire when WithScopedLockTTL is not supplied.
	DefaultScopedLockTTL = 30 * time.Second
	// DefaultScopedPollInterval is how long the generic scoped adapter waits
	// before its first re-try of a contended WithLock acquisition, when
	// WithScopedPollInterval is not supplied. Subsequent waits grow from here.
	DefaultScopedPollInterval = 100 * time.Millisecond
	// DefaultScopedPollBackoff is the factor each successive contended wait is
	// multiplied by, when WithScopedPollBackoff is not supplied.
	DefaultScopedPollBackoff = 2.0
	// DefaultScopedMaxPollInterval caps the grown wait, when
	// WithScopedPollBackoff is not supplied. It bounds how long a waiter can
	// sit idle after the lock actually frees, which is the cost backoff trades
	// against reduced load on the underlying store.
	DefaultScopedMaxPollInterval = time.Second

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

// WithScopedPollInterval sets the first wait WithLock takes after a contended
// acquisition; later waits grow from it per WithScopedPollBackoff. It must be
// positive — a non-positive interval would turn the wait into a spin, since
// clock.Sleep returns immediately for a non-positive duration — and
// NewScopedLocker rejects it. TryWithLock never polls.
func WithScopedPollInterval(interval time.Duration) ScopedOption {
	return func(s *scopedLocker) {
		s.pollInterval = interval
	}
}

// WithScopedPollBackoff sets how the contended wait grows: each successive
// wait is multiplied by factor and clamped to maxInterval.
//
// Backoff exists because a fixed poll interval makes N waiters on one key a
// thundering herd against the underlying store — N/interval requests per
// second for as long as the holder runs, none of which can succeed. Growing
// the interval trades a bounded amount of post-release latency (at most
// maxInterval, and on average less because of the jitter) for a large drop in
// that load.
//
// factor must be at least 1 and maxInterval at least the poll interval;
// NewScopedLocker rejects anything else. A factor of exactly 1 disables growth
// and restores a fixed interval, still jittered.
func WithScopedPollBackoff(factor float64, maxInterval time.Duration) ScopedOption {
	return func(s *scopedLocker) {
		s.pollBackoff = factor
		s.maxPollInterval = maxInterval
	}
}

// WithScopedJitter replaces the source of randomness that spreads contended
// waiters apart. fn must return a value in [0,1]; the adapter sleeps for half
// the current interval plus that fraction of the other half, so waiters that
// started together do not re-collide on every round.
//
// The default draws from math/rand/v2 and needs no seeding. Tests wanting a
// fixed schedule can pass func() float64 { return 1 }, which yields exactly
// the un-jittered interval. A nil fn is ignored.
func WithScopedJitter(fn func() float64) ScopedOption {
	return func(s *scopedLocker) {
		if fn != nil {
			s.jitter = fn
		}
	}
}

// WithScopedClock swaps the clock used for contention polling. Tests
// generally do not need it: under testing/synctest the default clock already
// runs on bubble time, so WithLock's waiting is deterministic and instant.
func WithScopedClock(c clock.Clock) ScopedOption {
	return func(s *scopedLocker) {
		if c != nil {
			s.clock = c
		}
	}
}

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) ScopedOption {
	return func(s *scopedLocker) {
		s.logger = logger
	}
}

// WithTracerProvider attaches a tracer provider, enabling spans on every
// scoped-lock operation. An absent tracer provider traces nowhere.
func WithTracerProvider(tracerProvider tracing.TracerProvider) ScopedOption {
	return func(s *scopedLocker) {
		s.tracerProvider = tracerProvider
	}
}

// WithMetricsProvider attaches a metrics provider for the scoped_lock_*
// counters and histograms. An absent provider records nothing.
func WithMetricsProvider(metricsProvider metrics.Provider) ScopedOption {
	return func(s *scopedLocker) {
		s.metricsProvider = metricsProvider
	}
}

// scopedLocker adapts a Locker into a ScopedLocker: acquire, run, release.
type scopedLocker struct {
	o11y            observability.Observer
	logger          logging.Logger
	tracerProvider  tracing.TracerProvider
	metricsProvider metrics.Provider
	locker          Locker
	clock           clock.Clock
	jitter          func() float64
	acquireCounter  metrics.Int64Counter
	contendCounter  metrics.Int64Counter
	errCounter      metrics.Int64Counter
	releaseFailures metrics.Int64Counter
	latencyHist     metrics.Float64Histogram
	waitHist        metrics.Float64Histogram
	ttl             time.Duration
	pollInterval    time.Duration
	maxPollInterval time.Duration
	pollBackoff     float64
}

// jitteredWait spreads waiters that started together, so they do not re-collide
// on every round. Equal jitter — half the interval, plus a random share of the
// other half — rather than full jitter, which can draw a near-zero wait and
// turn a backing-off poller back into a hot one.
func (s *scopedLocker) jitteredWait(interval time.Duration) time.Duration {
	half := interval / 2

	return half + time.Duration(s.jitter()*float64(half))
}

// grow advances the contended wait toward maxPollInterval. It is computed
// stepwise rather than as pollInterval*factor^n so a long wait cannot overflow
// the duration.
func (s *scopedLocker) grow(interval time.Duration) time.Duration {
	grown := time.Duration(float64(interval) * s.pollBackoff)
	if grown < interval {
		// Overflowed past the duration's range.
		return s.maxPollInterval
	}

	return min(grown, s.maxPollInterval)
}

var _ ScopedLocker = (*scopedLocker)(nil)

// NewScopedLocker wraps any Locker in scoped execution. WithLock waits for a
// contended lock by polling Acquire (the Locker atom deliberately has no
// queueing of its own); providers with native waiting (postgres) ship their
// own ScopedLocker and don't need this adapter.
//
// The scoped surface emits the same telemetry whichever Locker backs it —
// attach the observability deps via WithLogger, WithTracerProvider, and
// WithMetricsProvider: the wrapped Locker's own Acquire/Release
// instrumentation describes individual attempts, while scoped_lock_*
// describes the whole acquire-run-release operation, including fn's duration
// and the time spent waiting.
func NewScopedLocker(locker Locker, opts ...ScopedOption) (ScopedLocker, error) {
	if locker == nil {
		return nil, platformerrors.New("nil locker provided")
	}

	s := &scopedLocker{
		locker:          locker,
		clock:           clock.NewClock(),
		jitter:          rand.Float64,
		ttl:             DefaultScopedLockTTL,
		pollInterval:    DefaultScopedPollInterval,
		maxPollInterval: DefaultScopedMaxPollInterval,
		pollBackoff:     DefaultScopedPollBackoff,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}

	// The TTL is fixed for this locker's lifetime and every operation it reports
	// on is bounded by it, so it is stated here rather than at each Begin.
	s.o11y = observability.NewObserverWithValues(scopedServiceName, s.logger, s.tracerProvider,
		map[string]any{keys.LockTTLKey: s.ttl})

	mp := metrics.EnsureMetricsProvider(s.metricsProvider)

	var err error

	s.acquireCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_acquires", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating acquire counter")
	}
	s.contendCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_contended", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating contention counter")
	}
	s.errCounter, err = mp.NewInt64Counter(fmt.Sprintf("%s_errors", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating error counter")
	}
	s.releaseFailures, err = mp.NewInt64Counter(fmt.Sprintf("%s_release_failures", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating release failure counter")
	}
	s.latencyHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_latency_ms", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating latency histogram")
	}
	s.waitHist, err = mp.NewFloat64Histogram(fmt.Sprintf("%s_wait_ms", scopedServiceName))
	if err != nil {
		return nil, platformerrors.Wrap(err, "creating wait histogram")
	}

	// Validated here rather than in the options, which cannot report an error.
	// A non-positive poll interval is the dangerous one: clock.Sleep returns
	// immediately for it, so WithLock would spin on a contended lock instead
	// of waiting.
	switch {
	case s.pollInterval <= 0:
		return nil, platformerrors.Newf("scoped poll interval must be positive, got %s", s.pollInterval)
	case s.pollBackoff < 1:
		return nil, platformerrors.Newf("scoped poll backoff factor must be at least 1, got %v", s.pollBackoff)
	case s.maxPollInterval < s.pollInterval:
		return nil, platformerrors.Newf("scoped max poll interval (%s) must be at least the poll interval (%s)", s.maxPollInterval, s.pollInterval)
	}

	return s, nil
}

// WithLock implements ScopedLocker, waiting for a contended lock by polling.
func (s *scopedLocker) WithLock(ctx context.Context, key string, fn func(ctx context.Context) error) error {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(keys.LockKeyKey, key))
	defer op.End()

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
	wait := s.pollInterval
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

		if sleepErr := s.clock.Sleep(ctx, s.jitteredWait(wait)); sleepErr != nil {
			// Giving up still counts as contention — the caller waited and lost
			// — but a canceled or expired context is the caller's deadline
			// arriving, not an infrastructure failure, so it is traced without
			// incrementing the error counter.
			s.contendCounter.Add(ctx, 1)
			op.SpanOnly("lock.polls", polls)

			return op.Error(sleepErr, "waiting for scoped lock")
		}

		wait = s.grow(wait)
	}
}

// TryWithLock implements ScopedLocker.
func (s *scopedLocker) TryWithLock(ctx context.Context, key string, fn func(ctx context.Context) error) (bool, error) {
	ctx, op := s.o11y.Begin(ctx, observability.WithValue(keys.LockKeyKey, key))
	defer op.End()

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
