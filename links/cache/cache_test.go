package cache

import (
	"context"
	stderrors "errors"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/cache"
	cachemock "github.com/primandproper/platform-go/v14/cache/mock"
	platformerrors "github.com/primandproper/platform-go/v14/errors"
	"github.com/primandproper/platform-go/v14/links"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		s, err := New(newCache(t), newLocker(t))
		must.NoError(t, err)
		test.NotNil(t, s)
	})

	T.Run("rejects a nil cache", func(t *testing.T) {
		t.Parallel()

		_, err := New(nil, newLocker(t))
		test.ErrorIs(t, err, ErrNilCache)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	T.Run("rejects a nil locker", func(t *testing.T) {
		t.Parallel()

		// Without one, two concurrent redemptions of a token both see it
		// active. This store has no way to close that window, so it refuses to
		// be built without the thing that does.
		_, err := New(newCache(t), nil)
		test.ErrorIs(t, err, ErrNilLocker)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	T.Run("nil options are ignored", func(t *testing.T) {
		t.Parallel()

		_, err := New(newCache(t), newLocker(t), nil)
		test.NoError(t, err)
	})
}

func TestStore_Put(T *testing.T) {
	T.Parallel()

	T.Run("stores a record under the prefixed key", func(t *testing.T) {
		t.Parallel()

		s, c := newTestStore(t)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		record, err := c.Get(t.Context(), DefaultKeyPrefix+string(testID))
		must.NoError(t, err)
		test.EqOp(t, testAction, record.Action)
	})

	T.Run("takes its entry lifetime from the record rather than a clock", func(t *testing.T) {
		t.Parallel()

		var expiry time.Duration

		c := &cachemock.CacheMock[links.Record]{
			SetFunc: func(_ context.Context, _ string, _ *links.Record, opts ...cache.WriteOption) error {
				cfg := &cache.WriteConfig{}
				for _, opt := range opts {
					opt(cfg)
				}

				expiry = cfg.Expiry

				return nil
			},
		}

		s, err := New(c, newLocker(t))
		must.NoError(t, err)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		// PurgeAfter minus CreatedAt, exactly. Reading a clock here instead
		// would put a second one either side of the subtraction, and an entry
		// that expires before the link it holds is a link that answers
		// "not found" while it is still live.
		test.EqOp(t, 2*time.Hour, expiry)
	})

	T.Run("reports a cache that cannot be written", func(t *testing.T) {
		t.Parallel()

		s, err := New(&cachemock.CacheMock[links.Record]{
			SetFunc: func(context.Context, string, *links.Record, ...cache.WriteOption) error {
				return platformerrors.New("redis is on fire")
			},
		}, newLocker(t))
		must.NoError(t, err)

		test.Error(t, s.Put(t.Context(), testID, activeRecord()))
	})
}

func TestStore_Get(T *testing.T) {
	T.Parallel()

	T.Run("reads a record back whole", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		record, err := s.Get(t.Context(), testID)
		must.NoError(t, err)

		test.EqOp(t, testAction, record.Action)
		test.EqOp(t, testSubject, record.Subject)
		test.EqOp(t, links.StateActive, record.State)
		test.EqOp(t, mintedAt.Add(time.Hour), record.ExpiresAt)
		test.EqOp(t, mintedAt.Add(2*time.Hour), record.PurgeAfter)
	})

	T.Run("reports an absent record as not found", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		_, err := s.Get(t.Context(), testID)
		test.ErrorIs(t, err, links.ErrLinkNotFound)
	})

	T.Run("reports a record written by another version as stale", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		record := activeRecord()
		record.Version = links.RecordVersion + 1
		must.NoError(t, s.Put(t.Context(), testID, record))

		// Distinct from not-found on purpose: a Minter counts it, so a deploy
		// that changed the record shape shows up as one spike rather than as
		// links that mysteriously stopped working.
		_, err := s.Get(t.Context(), testID)
		test.ErrorIs(t, err, links.ErrStaleRecord)
		test.False(t, stderrors.Is(err, links.ErrLinkNotFound))
	})

	T.Run("does not answer an outage as a missing link", func(t *testing.T) {
		t.Parallel()

		// A read that said "no such link" during an outage would refuse every
		// redemption with a sentence claiming the link never existed, and a
		// Minter failing closed has to be able to tell the two apart.
		s, err := New(&cachemock.CacheMock[links.Record]{
			GetFunc: func(context.Context, string) (*links.Record, error) {
				return nil, cache.ErrUnavailable
			},
		}, newLocker(t))
		must.NoError(t, err)

		_, err = s.Get(t.Context(), testID)
		test.Error(t, err)
		test.False(t, stderrors.Is(err, links.ErrLinkNotFound))
	})

	T.Run("honors an empty key prefix", func(t *testing.T) {
		t.Parallel()

		s, c := newTestStore(t, WithKeyPrefix(""))

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		_, err := c.Get(t.Context(), string(testID))
		test.NoError(t, err)
	})
}

func TestStore_Resolve(T *testing.T) {
	T.Parallel()

	T.Run("transitions an active link", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		at := mintedAt.Add(time.Minute)

		record, err := s.Resolve(t.Context(), testID, links.StateRedeemed, at, at.Add(time.Hour))
		must.NoError(t, err)

		test.EqOp(t, links.StateRedeemed, record.State)
		test.EqOp(t, at, record.ResolvedAt)
		test.EqOp(t, at.Add(time.Hour), record.PurgeAfter)

		// And the transition is what the store holds afterwards, not just what
		// it answered with.
		stored, err := s.Get(t.Context(), testID)
		must.NoError(t, err)
		test.EqOp(t, links.StateRedeemed, stored.State)
	})

	T.Run("refuses a second transition and says which happened", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		at := mintedAt.Add(time.Minute)

		_, err := s.Resolve(t.Context(), testID, links.StateRevoked, at, at.Add(time.Hour))
		must.NoError(t, err)

		record, err := s.Resolve(t.Context(), testID, links.StateRedeemed, at, at.Add(time.Hour))
		test.ErrorIs(t, err, links.ErrLinkRevoked)

		// The record travels with the refusal: its action is what keeps a
		// metric labeled by action from going blank exactly when one flow's
		// links start failing.
		must.NotNil(t, record)
		test.EqOp(t, testAction, record.Action)
	})

	T.Run("refuses a link past its expiry without writing", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		at := mintedAt.Add(2 * time.Hour)

		_, err := s.Resolve(t.Context(), testID, links.StateRedeemed, at, at.Add(time.Hour))
		test.ErrorIs(t, err, links.ErrLinkExpired)

		stored, err := s.Get(t.Context(), testID)
		must.NoError(t, err)
		test.EqOp(t, links.StateActive, stored.State)
	})

	T.Run("reports an absent link", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		_, err := s.Resolve(t.Context(), testID, links.StateRedeemed, mintedAt, mintedAt.Add(time.Hour))
		test.ErrorIs(t, err, links.ErrLinkNotFound)
	})

	T.Run("reports a record written by another version as stale", func(t *testing.T) {
		t.Parallel()

		s, _ := newTestStore(t)

		record := activeRecord()
		record.Version = links.RecordVersion + 1
		must.NoError(t, s.Put(t.Context(), testID, record))

		_, err := s.Resolve(t.Context(), testID, links.StateRedeemed, mintedAt, mintedAt.Add(time.Hour))
		test.ErrorIs(t, err, links.ErrStaleRecord)
	})

	T.Run("exactly one of many concurrent transitions succeeds", func(t *testing.T) {
		t.Parallel()

		// The reason the locker is a required argument. Without mutual
		// exclusion every one of these reads the link active and every one of
		// them writes, and the failure appears only under concurrency.
		s, _ := newTestStore(t)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		const attempts = 16

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			resolved int
		)

		start := make(chan struct{})
		at := mintedAt.Add(time.Minute)

		for range attempts {
			wg.Go(func() {
				<-start

				if _, err := s.Resolve(context.WithoutCancel(t.Context()),
					testID, links.StateRedeemed, at, at.Add(time.Hour)); err == nil {
					mu.Lock()
					resolved++
					mu.Unlock()
				}
			})
		}

		close(start)
		wg.Wait()

		test.EqOp(t, 1, resolved)
	})

	T.Run("does not commit a transition the cache refused to write", func(t *testing.T) {
		t.Parallel()

		// Fails closed: the link is live and cannot be marked spent, so nothing
		// may be handed back that reads as a successful redemption.
		var stored *links.Record

		c := &cachemock.CacheMock[links.Record]{
			SetFunc: func(_ context.Context, _ string, value *links.Record, _ ...cache.WriteOption) error {
				if stored == nil {
					stored = value

					return nil
				}

				return platformerrors.New("redis is on fire")
			},
			GetFunc: func(context.Context, string) (*links.Record, error) {
				if stored == nil {
					return nil, cache.ErrNotFound
				}

				return stored, nil
			},
		}

		s, err := New(c, newLocker(t))
		must.NoError(t, err)

		must.NoError(t, s.Put(t.Context(), testID, activeRecord()))

		at := mintedAt.Add(time.Minute)

		record, err := s.Resolve(t.Context(), testID, links.StateRedeemed, at, at.Add(time.Hour))
		test.Error(t, err)
		test.Nil(t, record)
		test.False(t, stderrors.Is(err, links.ErrLinkAlreadyRedeemed))
	})
}
