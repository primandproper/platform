package distributedlock

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/primandproper/platform-go/v7/clock"
	platformerrors "github.com/primandproper/platform-go/v7/errors"
)

const (
	// DefaultScopedLockTTL is the TTL the generic scoped adapter passes to
	// Acquire when WithScopedLockTTL is not supplied.
	DefaultScopedLockTTL = 30 * time.Second
	// DefaultScopedPollInterval is how often the generic scoped adapter re-tries
	// a contended WithLock acquisition when WithScopedPollInterval is not
	// supplied.
	DefaultScopedPollInterval = 100 * time.Millisecond
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
// implicit release will surface ErrLockNotHeld in the returned error.
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
	locker       Locker
	clock        clock.Clock
	ttl          time.Duration
	pollInterval time.Duration
}

var _ ScopedLocker = (*scopedLocker)(nil)

// NewScopedLocker wraps any Locker in scoped execution. WithLock waits for a
// contended lock by polling Acquire (the Locker atom deliberately has no
// queueing of its own); providers with native waiting (postgres) ship their
// own ScopedLocker and don't need this adapter.
func NewScopedLocker(locker Locker, opts ...ScopedOption) (ScopedLocker, error) {
	if locker == nil {
		return nil, platformerrors.New("nil locker provided")
	}

	s := &scopedLocker{
		locker:       locker,
		clock:        clock.NewClock(),
		ttl:          DefaultScopedLockTTL,
		pollInterval: DefaultScopedPollInterval,
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
	for {
		held, err := s.locker.Acquire(ctx, key, s.ttl)
		if err == nil {
			return s.run(ctx, held, fn)
		}
		if !stderrors.Is(err, ErrLockNotAcquired) {
			return err
		}

		if sleepErr := s.clock.Sleep(ctx, s.pollInterval); sleepErr != nil {
			return sleepErr
		}
	}
}

// TryWithLock implements ScopedLocker.
func (s *scopedLocker) TryWithLock(ctx context.Context, key string, fn func(ctx context.Context) error) (bool, error) {
	held, err := s.locker.Acquire(ctx, key, s.ttl)
	if stderrors.Is(err, ErrLockNotAcquired) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, s.run(ctx, held, fn)
}

// run executes fn and releases held afterward, panics included. The release
// uses a non-cancelable context so a canceled caller can't strand the lock
// until TTL expiry.
func (s *scopedLocker) run(ctx context.Context, held Lock, fn func(ctx context.Context) error) (err error) {
	defer func() {
		if releaseErr := held.Release(context.WithoutCancel(ctx)); releaseErr != nil {
			err = platformerrors.Join(err, platformerrors.Wrap(releaseErr, "releasing scoped lock"))
		}
	}()

	return fn(ctx)
}
