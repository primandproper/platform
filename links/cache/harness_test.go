package cache

import (
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/cache"
	cachememory "github.com/primandproper/platform-go/v14/cache/memory"
	"github.com/primandproper/platform-go/v14/distributedlock"
	dlmemory "github.com/primandproper/platform-go/v14/distributedlock/memory"
	"github.com/primandproper/platform-go/v14/links"

	"github.com/shoenig/test/must"
)

const (
	testID      links.ID      = "8b1a9953c4611296a827abf8c47804d7"
	testAction  links.Action  = "magic_login"
	testSubject links.Subject = "user_123"
)

// mintedAt is the instant every record in this package's tests is written at,
// so a test can name an expiry relative to it without reading a clock.
var mintedAt = time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)

// newCache builds an in-memory cache. Expiry is per-write, so the cache default
// is irrelevant.
func newCache(tb testing.TB) cache.Cache[links.Record] {
	tb.Helper()

	c, err := cachememory.NewInMemoryCache[links.Record](0)
	must.NoError(tb, err)

	return c
}

// newLocker builds a real in-process scoped locker, so the concurrency test
// exercises actual mutual exclusion rather than a double that always grants.
func newLocker(tb testing.TB) distributedlock.ScopedLocker {
	tb.Helper()

	locker, err := dlmemory.NewLocker()
	must.NoError(tb, err)

	scoped, err := distributedlock.NewScopedLocker(locker)
	must.NoError(tb, err)

	return scoped
}

// newTestStore builds a store over a fresh cache and locker, handing back the
// cache so a test can read or corrupt what was written.
func newTestStore(tb testing.TB, opts ...Option) (*Store, cache.Cache[links.Record]) {
	tb.Helper()

	c := newCache(tb)

	s, err := New(c, newLocker(tb), opts...)
	must.NoError(tb, err)

	return s, c
}

// activeRecord is a link that is live for an hour and collectable an hour after
// that.
func activeRecord() *links.Record {
	return &links.Record{
		CreatedAt:  mintedAt,
		ExpiresAt:  mintedAt.Add(time.Hour),
		PurgeAfter: mintedAt.Add(2 * time.Hour),
		Action:     testAction,
		Subject:    testSubject,
		Version:    links.RecordVersion,
		State:      links.StateActive,
	}
}
