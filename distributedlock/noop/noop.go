package noop

import (
	"context"
	"time"

	"github.com/primandproper/platform-go/v9/distributedlock"
)

var (
	_ distributedlock.Locker = (*locker)(nil)
	_ distributedlock.Lock   = (*lock)(nil)
)

// locker is a no-op distributedlock.Locker. Acquire always succeeds, Release and
// Refresh are no-ops, Ping returns nil. Use this when distributed locking is not
// needed in a given deployment (single replica, dev environments, etc.).
type locker struct{}

// NewLocker returns a no-op Locker.
func NewLocker() distributedlock.Locker {
	return &locker{}
}

// Acquire always returns a trivial lock handle.
func (*locker) Acquire(_ context.Context, key string, ttl time.Duration) (distributedlock.Lock, error) {
	return &lock{key: key, ttl: ttl}, nil
}

// Ping is a no-op that always succeeds.
func (*locker) Ping(_ context.Context) error {
	return nil
}

// Close is a no-op.
func (*locker) Close() error {
	return nil
}

// lock is a trivial Lock implementation paired with the noop locker.
type lock struct {
	key string
	ttl time.Duration
}

// Key returns the lock key.
func (l *lock) Key() string {
	return l.key
}

// TTL returns the configured TTL.
func (l *lock) TTL() time.Duration {
	return l.ttl
}

// Release is a no-op.
func (*lock) Release(_ context.Context) error {
	return nil
}

// Refresh updates the configured TTL but does no work.
func (l *lock) Refresh(_ context.Context, ttl time.Duration) error {
	l.ttl = ttl
	return nil
}

var _ distributedlock.ScopedLocker = (*scopedLocker)(nil)

// scopedLocker is a no-op distributedlock.ScopedLocker: fn always runs, as if
// the lock were acquired immediately. Use it where scoped locking is wired but
// a deployment has nothing to coordinate with (single replica, dev).
type scopedLocker struct{}

// NewScopedLocker returns a no-op ScopedLocker.
func NewScopedLocker() distributedlock.ScopedLocker {
	return &scopedLocker{}
}

// WithLock runs fn immediately.
func (*scopedLocker) WithLock(ctx context.Context, _ string, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

// TryWithLock runs fn immediately, always reporting the lock as acquired.
func (*scopedLocker) TryWithLock(ctx context.Context, _ string, fn func(ctx context.Context) error) (bool, error) {
	return true, fn(ctx)
}
