package links

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v14/cryptography/hashing"
	"github.com/primandproper/platform-go/v14/cryptography/hashing/sha256"
	platformerrors "github.com/primandproper/platform-go/v14/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewMinter(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m, err := NewMinter(newMemoryStore(), WithAction(testAction, testPolicy()))
		must.NoError(t, err)
		test.NotNil(t, m)
	})

	T.Run("rejects a nil store", func(t *testing.T) {
		t.Parallel()

		_, err := NewMinter(nil, WithAction(testAction, testPolicy()))
		test.ErrorIs(t, err, ErrNilStore)
		test.ErrorIs(t, err, platformerrors.ErrNilInputParameter)
	})

	T.Run("rejects a Minter with no actions", func(t *testing.T) {
		t.Parallel()

		_, err := NewMinter(newMemoryStore())
		test.ErrorIs(t, err, ErrNoActions)
	})

	T.Run("rejects an action with an invalid policy", func(t *testing.T) {
		t.Parallel()

		_, err := NewMinter(newMemoryStore(), WithAction(testAction, ActionPolicy{
			URL: "https://app.example.com/auth/magic/{token}",
		}))
		test.ErrorIs(t, err, ErrInvalidTTL)
	})

	T.Run("reports the registered actions", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t, WithAction("verify_email", ActionPolicy{
			URL: "https://app.example.com/verify?t={token}",
			TTL: time.Hour,
		}))

		test.SliceLen(t, 2, m.Actions())
		test.SliceContains(t, m.Actions(), testAction)
	})
}

func TestMinter_Mint(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		test.EqOp(t, testAction, link.Action)
		test.EqOp(t, testSubject, link.Subject)
		test.True(t, strings.HasPrefix(link.URL, "https://app.example.com/auth/magic/"))
		test.StrHasSuffix(t, string(link.Token), link.URL)
		test.NotEq(t, "", string(link.ID))
	})

	T.Run("the ID is the digest of the token and not the token", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		// The relationship the whole design rests on: what is stored and spoken
		// about is derived from the secret and does not contain it.
		test.EqOp(t, ID(hashing.HexString(sha256.NewSHA256Hasher(), string(link.Token))), link.ID)
		test.False(t, strings.Contains(string(link.ID), string(link.Token)))
	})

	T.Run("stores no token anywhere in the record", func(t *testing.T) {
		t.Parallel()

		m, store := newTestMinterStore(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		// Read back under the ID, which is itself the assertion that the store
		// is keyed by the digest.
		record := store.stored(t, link.ID)

		test.EqOp(t, StateActive, record.State)
		test.EqOp(t, testAction, record.Action)
		test.EqOp(t, testSubject, record.Subject)
		test.False(t, strings.Contains(rendered(record), string(link.Token)))
	})

	T.Run("sets the purge deadline past the link's expiry", func(t *testing.T) {
		t.Parallel()

		c := newTestClock()
		m, store := newTestMinterStore(t, WithClock(c.Clock()), WithRetention(time.Hour))

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		// The gap is what buys "this link has already been used" over "no such
		// link" — the store may forget the record only after it.
		test.EqOp(t, link.ExpiresAt.Add(time.Hour), store.stored(t, link.ID).PurgeAfter)
	})

	T.Run("mints two different tokens for the same subject", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		first, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		second, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		test.NotEq(t, string(first.Token), string(second.Token))
		test.NotEq(t, string(first.ID), string(second.ID))
	})

	T.Run("rejects an unregistered action", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		_, err := m.Mint(t.Context(), "magic_logn", testSubject)
		test.ErrorIs(t, err, ErrUnknownAction)
	})

	T.Run("rejects an empty subject", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		_, err := m.Mint(t.Context(), testAction, "")
		test.ErrorIs(t, err, ErrEmptySubject)
	})

	T.Run("expires at the action's TTL", func(t *testing.T) {
		t.Parallel()

		c := newTestClock()
		m := newTestMinter(t, WithClock(c.Clock()))

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		test.EqOp(t, c.Clock().Now().UTC().Add(testActionTTL), link.ExpiresAt)
	})

	T.Run("a per-call TTL overrides the action's", func(t *testing.T) {
		t.Parallel()

		c := newTestClock()
		m := newTestMinter(t, WithClock(c.Clock()))

		link, err := m.Mint(t.Context(), testAction, testSubject, WithTTL(time.Minute))
		must.NoError(t, err)

		test.EqOp(t, c.Clock().Now().UTC().Add(time.Minute), link.ExpiresAt)
	})

	T.Run("puts the token in a query template", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t, WithAction("unsubscribe", ActionPolicy{
			URL: "https://app.example.com/unsubscribe?t={token}",
			TTL: 365 * 24 * time.Hour,
		}))

		link, err := m.Mint(t.Context(), "unsubscribe", testSubject)
		must.NoError(t, err)

		test.EqOp(t, "https://app.example.com/unsubscribe?t="+string(link.Token), link.URL)
	})

	T.Run("reports a store failure rather than a link nothing recorded", func(t *testing.T) {
		t.Parallel()

		m := newFailingStoreMinter(t, platformerrors.New("redis is on fire"))

		_, err := m.Mint(t.Context(), testAction, testSubject)
		test.ErrorIs(t, err, ErrStoreUnavailable)
	})
}

func TestMinter_Redeem(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject, WithMetadata(map[string]string{"next": "/dashboard"}))
		must.NoError(t, err)

		claims, err := m.Redeem(t.Context(), link.Token)
		must.NoError(t, err)

		test.EqOp(t, testAction, claims.Action)
		test.EqOp(t, testSubject, claims.Subject)
		test.EqOp(t, link.ID, claims.ID)
		test.EqOp(t, link.ExpiresAt, claims.ExpiresAt)
		test.EqOp(t, "/dashboard", claims.Metadata["next"])
	})

	T.Run("a second redemption is refused", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		_, err = m.Redeem(t.Context(), link.Token)
		must.NoError(t, err)

		_, err = m.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkAlreadyRedeemed)
	})

	T.Run("exactly one of many concurrent redemptions succeeds", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		const attempts = 16

		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			redeemed int
		)

		start := make(chan struct{})

		for range attempts {
			wg.Go(func() {
				<-start

				if _, redeemErr := m.Redeem(context.WithoutCancel(t.Context()), link.Token); redeemErr == nil {
					mu.Lock()
					redeemed++
					mu.Unlock()
				}
			})
		}

		close(start)
		wg.Wait()

		// The Store contract, from above it: whatever a store buys atomicity
		// with, exactly one caller may be told it holds the link. Without it
		// this is a number greater than one, and only under concurrency —
		// which is what links/cache's locker and links/database's guarded
		// UPDATE are each tested for in their own package.
		test.EqOp(t, 1, redeemed)
	})

	T.Run("refuses a link past its expiry", func(t *testing.T) {
		t.Parallel()

		c := newTestClock()
		m := newTestMinter(t, WithClock(c.Clock()))

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		c.Advance(testActionTTL + time.Second)

		_, err = m.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkExpired)
	})

	T.Run("refuses a link at the instant it expires", func(t *testing.T) {
		t.Parallel()

		c := newTestClock()
		m := newTestMinter(t, WithClock(c.Clock()))

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		c.Advance(testActionTTL)

		// The boundary belongs to the dead side: ExpiresAt is when it stops
		// working, not the last moment it works.
		_, err = m.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkExpired)
	})

	T.Run("refuses an unknown token", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		_, err := m.Redeem(t.Context(), "not-a-token-anybody-minted")
		test.ErrorIs(t, err, ErrLinkNotFound)
	})

	T.Run("refuses an empty token without touching the store", func(t *testing.T) {
		t.Parallel()

		m := newFailingStoreMinter(t, platformerrors.New("should not be reached"))

		_, err := m.Redeem(t.Context(), "")
		test.ErrorIs(t, err, ErrInvalidToken)
	})

	T.Run("refuses an over-long token without hashing it", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t, WithMaxTokenLength(16))

		_, err := m.Redeem(t.Context(), Token(strings.Repeat("a", 17)))
		test.ErrorIs(t, err, ErrInvalidToken)
	})

	T.Run("fails closed when the consuming write cannot land", func(t *testing.T) {
		t.Parallel()

		// A store that mints fine and cannot resolve: the link is valid and
		// cannot be marked spent. Handing back claims here would be single use
		// failing open, which is the one thing that must not happen.
		m, store := newTestMinterStore(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		store.resolveErr = platformerrors.New("redis is on fire")

		claims, err := m.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrStoreUnavailable)
		test.Nil(t, claims)
	})

	T.Run("fails closed when the store cannot be read", func(t *testing.T) {
		t.Parallel()

		m := newFailingStoreMinter(t, platformerrors.New("redis is on fire"))

		_, err := m.Redeem(t.Context(), "some-token")
		test.ErrorIs(t, err, ErrStoreUnavailable)
	})

	T.Run("ignores a record written by another version", func(t *testing.T) {
		t.Parallel()

		m, store := newTestMinterStore(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		store.stored(t, link.ID).Version = RecordVersion + 1

		// A record this binary cannot read means the same thing to a bearer as
		// no record at all, and the store's ErrStaleRecord is what gets it
		// there rather than being reported as an outage.
		_, err = m.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkNotFound)
	})

	T.Run("reports a record written by another version as absent to Inspect too", func(t *testing.T) {
		t.Parallel()

		m, store := newTestMinterStore(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		store.stored(t, link.ID).Version = RecordVersion + 1

		_, err = m.Inspect(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkNotFound)
	})

	T.Run("hands back a metadata map the store does not share", func(t *testing.T) {
		t.Parallel()

		m, store := newTestMinterStore(t)

		link, err := m.Mint(t.Context(), testAction, testSubject, WithMetadata(map[string]string{"next": "/dashboard"}))
		must.NoError(t, err)

		claims, err := m.Redeem(t.Context(), link.Token)
		must.NoError(t, err)

		claims.Metadata["next"] = "/evil"

		// The store double hands back the pointer it holds, as the memory cache
		// provider does, so a shared map would have let that assignment edit
		// the stored record.
		test.EqOp(t, "/dashboard", store.stored(t, link.ID).Metadata["next"])
	})
}

func TestMinter_Inspect(T *testing.T) {
	T.Parallel()

	T.Run("reports the claims without consuming the link", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		claims, err := m.Inspect(t.Context(), link.Token)
		must.NoError(t, err)
		test.EqOp(t, testSubject, claims.Subject)

		// The reason Inspect exists: a mail scanner's GET must leave the link
		// intact for the person the message was sent to.
		_, err = m.Redeem(t.Context(), link.Token)
		test.NoError(t, err)
	})

	T.Run("reports the same refusals redemption would", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		_, err = m.Redeem(t.Context(), link.Token)
		must.NoError(t, err)

		_, err = m.Inspect(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkAlreadyRedeemed)
	})

	T.Run("refuses an unknown token", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		_, err := m.Inspect(t.Context(), "not-a-token-anybody-minted")
		test.ErrorIs(t, err, ErrLinkNotFound)
	})

	T.Run("refuses an empty token", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		_, err := m.Inspect(t.Context(), "")
		test.ErrorIs(t, err, ErrInvalidToken)
	})
}

func TestMinter_Revoke(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		must.NoError(t, m.Revoke(t.Context(), link.ID))

		_, err = m.Redeem(t.Context(), link.Token)
		test.ErrorIs(t, err, ErrLinkRevoked)
	})

	T.Run("revoking twice is not an error", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		must.NoError(t, m.Revoke(t.Context(), link.ID))
		test.NoError(t, m.Revoke(t.Context(), link.ID))
	})

	T.Run("reports that a revoked link had already been used", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		_, err = m.Redeem(t.Context(), link.Token)
		must.NoError(t, err)

		// An operator revoking after a suspected compromise has to learn they
		// were too late; a nil here would say the opposite.
		test.ErrorIs(t, m.Revoke(t.Context(), link.ID), ErrLinkAlreadyRedeemed)
	})

	T.Run("refuses an empty ID", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		test.ErrorIs(t, m.Revoke(t.Context(), ""), ErrInvalidID)
	})

	T.Run("reports an unknown ID", func(t *testing.T) {
		t.Parallel()

		m := newTestMinter(t)

		test.ErrorIs(t, m.Revoke(t.Context(), "0000"), ErrLinkNotFound)
	})

	T.Run("an expired link needs no revoking", func(t *testing.T) {
		t.Parallel()

		c := newTestClock()
		m := newTestMinter(t, WithClock(c.Clock()))

		link, err := m.Mint(t.Context(), testAction, testSubject)
		must.NoError(t, err)

		c.Advance(testActionTTL + time.Second)

		test.NoError(t, m.Revoke(t.Context(), link.ID))
	})
}

// newFailingStoreMinter builds a Minter whose store fails every operation, for
// the cases that must not degrade into a working-looking answer.
func newFailingStoreMinter(tb testing.TB, storeErr error) *Minter {
	tb.Helper()

	store := newMemoryStore()
	store.getErr, store.putErr, store.resolveErr = storeErr, storeErr, storeErr

	m, err := NewMinter(store, WithAction(testAction, testPolicy()))
	must.NoError(tb, err)

	return m
}

// rendered flattens a record into one string, so a test can assert that a
// secret appears nowhere in it rather than field by field.
func rendered(record *Record) string {
	parts := []string{
		record.CreatedAt.String(),
		record.ExpiresAt.String(),
		record.ResolvedAt.String(),
		string(record.Action),
		string(record.Subject),
	}
	for k, v := range record.Metadata {
		parts = append(parts, k, v)
	}

	return strings.Join(parts, "|")
}
