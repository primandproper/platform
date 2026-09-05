package cache

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/primandproper/platform-go/v14/cache"
	"github.com/primandproper/platform-go/v14/distributedlock"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/links"
	"github.com/primandproper/platform-go/v14/observability"
)

// serviceName names the loggers and spans this store emits. The counters live
// on the Minter, which is the layer that knows what an operation meant.
const serviceName = "links_cache"

var _ links.Store = (*Store)(nil)

// Store keeps action link records in a cache.Cache, with a distributed lock
// making the read and the write of a resolution one operation.
//
// It is exported, and returned by New, so a caller who has chosen cache-backed
// links can depend on that choice rather than on the links.Store seam.
type Store struct {
	cache  cache.Cache[links.Record]
	locker distributedlock.ScopedLocker
	o11y   observability.Observer

	keyPrefix string
}

// New builds a links.Store over a cache and a locker.
//
// Both are required and neither has a default. Which cache it is decides
// whether the links work at all: a link is minted by whatever builds the email
// and redeemed by whatever serves the click, and cache/memory is per-process,
// so those two see different stores unless they are the same process — and even
// then every outstanding link dies at the next restart. Redis is the production
// answer; memory is for tests and for a single process that accepts both.
//
// The locker is what makes a redemption single-use here; see ErrNilLocker.
//
// The cache's own default expiry is never used — every write carries the window
// the Minter computed from the record — so a cache built solely for links can
// be configured with any expiry at all.
func New(
	c cache.Cache[links.Record],
	locker distributedlock.ScopedLocker,
	opts ...Option,
) (*Store, error) {
	if c == nil {
		return nil, ErrNilCache
	}
	if locker == nil {
		return nil, ErrNilLocker
	}

	o := newOptions(opts)

	s := &Store{
		cache:     c,
		locker:    locker,
		keyPrefix: DefaultKeyPrefix,
		o11y:      observability.NewObserver(serviceName, o.logger, o.tracerProvider),
	}

	if o.keyPrefix != nil {
		s.keyPrefix = *o.keyPrefix
	}

	return s, nil
}

// Put writes a freshly minted link's record.
//
// The entry's lifetime is the record's own — the span between the moment it was
// created and the moment the store may forget it — so this reads no clock. Two
// clocks either side of that subtraction is how an entry ends up expiring
// before the link it holds does.
func (s *Store) Put(ctx context.Context, id links.ID, record *links.Record) error {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	if err := s.cache.Set(ctx, s.key(id), record,
		cache.WithExpiry(record.PurgeAfter.Sub(record.CreatedAt))); err != nil {
		return op.Error(err, "storing action link record")
	}

	return nil
}

// Get reads a record without consuming it.
func (s *Store) Get(ctx context.Context, id links.ID) (*links.Record, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	return s.read(ctx, op, id)
}

// Resolve moves an active link into a terminal state under a lock on that link.
//
// The read and the write are both inside the lock, which is the whole of the
// guarantee: a check that passes and a write that lands separately is exactly
// the window in which two requests carrying one token both see it active.
//
// The lock key is deliberately distinct from the record key. The two live in
// different systems — a locker is not this cache, and in production is often
// not even the same Redis — and a shared spelling invites the assumption that
// one can be derived from the other.
//
// A link that answers for itself leaves the lock through the callback's error
// return, and is separated from a failure of the store afterwards. Nothing is
// lost by that: every one of those answers is reached before the write, so
// there is no half-done transition to unwind, and the record travels out on
// found — the action it names is what keeps a metric labeled by action from
// going blank exactly when one flow's links start failing.
func (s *Store) Resolve(
	ctx context.Context,
	id links.ID,
	to links.State,
	at, purgeAfter time.Time,
) (*links.Record, error) {
	ctx, op := s.o11y.Begin(ctx)
	defer op.End()

	var found *links.Record

	if err := s.locker.WithLock(ctx, s.lockKey(id), func(ctx context.Context) error {
		record, readErr := s.read(ctx, op, id)
		if readErr != nil {
			return readErr
		}

		found = record

		// Decided inside the lock rather than after it, so the answer and the
		// write that depends on it cannot be separated by another caller's
		// transition.
		if usableErr := record.Usable(at); usableErr != nil {
			return usableErr
		}

		// Copied rather than mutated in place: the memory cache provider hands
		// back the live pointer it holds, so writing through record would edit
		// the stored link before the write that is supposed to commit the edit.
		resolved := *record
		resolved.State = to
		resolved.ResolvedAt = at
		resolved.PurgeAfter = purgeAfter

		if setErr := s.cache.Set(ctx, s.key(id), &resolved,
			cache.WithExpiry(purgeAfter.Sub(at))); setErr != nil {
			return setErr
		}

		found = &resolved

		return nil
	}); err != nil {
		if isStoreAnswer(err) {
			return found, err
		}

		return nil, op.Error(err, "resolving action link record")
	}

	return found, nil
}

// read resolves an ID to the record stored under it.
//
// It is shared by Get and by the body of Resolve's lock so that the two cannot
// come to disagree about what an absent, an unreadable, and an unavailable
// store look like — the three answers whose consequences are furthest apart.
func (s *Store) read(
	ctx context.Context,
	op observability.Operation,
	id links.ID,
) (*links.Record, error) {
	record, err := s.cache.Get(ctx, s.key(id))
	switch {
	case err == nil:
	case stderrors.Is(err, cache.ErrNotFound):
		return nil, links.ErrLinkNotFound
	default:
		// cache.ErrUnavailable is deliberately not folded into the absent case.
		// A read that answered "no such link" during an outage would refuse
		// every redemption with a sentence saying the link never existed, and a
		// redemption that fails closed has to say which of the two it was.
		return nil, op.Error(err, "reading action link record")
	}

	if record == nil {
		return nil, links.ErrLinkNotFound
	}

	if !record.Current() {
		return nil, platformerrors.Wrapf(links.ErrStaleRecord, "record version %d", record.Version)
	}

	return record, nil
}

// isStoreAnswer reports whether err is the store answering about a link rather
// than failing to answer.
//
// The two travel out of the lock together and are separated here, because the
// consequences are opposite: a link that is spent is this package working, and
// a cache that cannot be read is a redemption that did not happen. Everything
// unrecognized is the second kind, which is the direction that fails closed.
func isStoreAnswer(err error) bool {
	return stderrors.Is(err, links.ErrLinkNotFound) ||
		stderrors.Is(err, links.ErrStaleRecord) ||
		stderrors.Is(err, links.ErrLinkAlreadyRedeemed) ||
		stderrors.Is(err, links.ErrLinkExpired) ||
		stderrors.Is(err, links.ErrLinkRevoked)
}

// key namespaces a link's ID for the record store.
func (s *Store) key(id links.ID) string {
	return s.keyPrefix + string(id)
}

// lockKey namespaces a link's ID for the locker.
func (s *Store) lockKey(id links.ID) string {
	return s.keyPrefix + "lock:" + string(id)
}
