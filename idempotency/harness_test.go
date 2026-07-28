package idempotency

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/cache"
	cachememory "github.com/primandproper/platform-go/v8/cache/memory"
	cachemock "github.com/primandproper/platform-go/v8/cache/mock"
	"github.com/primandproper/platform-go/v8/distributedlock"
	dlmemory "github.com/primandproper/platform-go/v8/distributedlock/memory"
	dlmock "github.com/primandproper/platform-go/v8/distributedlock/mock"

	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/metric"
)

const (
	testKey         = "test-key"
	testFingerprint = "test-fingerprint"
)

// payload is the recorded value in these tests. It is a concrete struct with
// exported fields, which is what the store can round-trip.
type payload struct {
	Name string
}

// newStore builds an in-memory record store. The expiry is per-write, so the
// cache default is irrelevant.
func newStore(tb testing.TB) cache.Cache[Record[payload]] {
	tb.Helper()

	c, err := cachememory.NewInMemoryCache[Record[payload]](0, nil, nil, nil)
	must.NoError(tb, err)

	return c
}

// newLocker builds a real in-process scoped locker, so concurrency tests
// exercise actual mutual exclusion rather than a mock that always grants.
func newLocker(tb testing.TB) distributedlock.ScopedLocker {
	tb.Helper()

	locker, err := dlmemory.NewLocker(nil, nil, nil)
	must.NoError(tb, err)

	scoped, err := distributedlock.NewScopedLocker(locker, nil, nil, nil)
	must.NoError(tb, err)

	return scoped
}

// newTestManager builds a Manager over a memory store and a memory locker.
func newTestManager(tb testing.TB, opts ...Option[payload]) *Manager[payload] {
	tb.Helper()

	m, err := NewManager(newStore(tb), newLocker(tb), opts...)
	must.NoError(tb, err)

	return m
}

// countingFn records how many times the work ran, so a replay can be
// distinguished from a re-execution by something other than its result.
type countingFn struct {
	value *payload
	err   error
	calls int64
	mu    sync.Mutex
}

func newCountingFn(name string) *countingFn {
	return &countingFn{value: &payload{Name: name}}
}

func (f *countingFn) run(context.Context) (*payload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	return f.value, f.err
}

func (f *countingFn) Calls() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

// countingCounter records what an instrument was asked to add, so a test can
// assert a counter fired without standing up an SDK metrics pipeline.
type countingCounter struct {
	mu    sync.Mutex
	total int64
}

func (c *countingCounter) Add(_ context.Context, incr int64, _ ...metric.AddOption) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.total += incr
}

func (c *countingCounter) Total() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.total
}

// failingStore wraps a real store and fails Get with err.
//
// It is built on the generated mock so that any method a test did not expect
// to be called panics rather than silently returning a zero value.
func failingStore(tb testing.TB, err error) *cachemock.CacheMock[Record[payload]] {
	tb.Helper()

	return &cachemock.CacheMock[Record[payload]]{
		GetFunc: func(context.Context, string) (*Record[payload], error) {
			return nil, err
		},
	}
}

// grantingLocker runs fn without any locking, optionally doing something first.
//
// before is where a test injects a racer: whatever it writes to the store lands
// between Do's pre-lock read and the claim, which is the interleaving the
// double-check exists to survive.
func grantingLocker(before func(ctx context.Context)) *dlmock.ScopedLockerMock {
	return &dlmock.ScopedLockerMock{
		WithLockFunc: func(ctx context.Context, _ string, fn func(ctx context.Context) error) error {
			if before != nil {
				before(ctx)
			}

			return fn(ctx)
		},
	}
}

// seed writes a record directly, standing in for whatever another process left
// behind.
func seed(t *testing.T, m *Manager[payload], key string, record *Record[payload], expiry time.Duration) {
	t.Helper()

	must.NoError(t, m.store.Set(t.Context(), m.storeKey(key), record, cache.WithExpiry(expiry)))
}

// completed builds a finished record for fingerprint.
func completed(fingerprint, name string) *Record[payload] {
	return &Record[payload]{
		CreatedAt:   time.Now().UTC(),
		Value:       &payload{Name: name},
		Fingerprint: fingerprint,
		ClaimID:     "seeded",
		Version:     recordVersion,
		State:       StateCompleted,
	}
}

// inFlight builds a claim record for fingerprint.
func inFlight(fingerprint string) *Record[payload] {
	return &Record[payload]{
		CreatedAt:   time.Now().UTC(),
		Fingerprint: fingerprint,
		ClaimID:     "seeded",
		Version:     recordVersion,
		State:       StateInFlight,
	}
}
