package idempotency

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v8/cache"
	cachemock "github.com/primandproper/platform-go/v8/cache/mock"
	platformerrors "github.com/primandproper/platform-go/v8/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewManager(T *testing.T) {
	T.Parallel()

	T.Run("applies defaults", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t)

		test.EqOp(t, DefaultTTL, m.ttl)
		test.EqOp(t, DefaultInFlightTTL, m.inFlightTTL)
		test.EqOp(t, DefaultMaxKeyLength, m.maxKeyLength)
		test.EqOp(t, DefaultKeyPrefix, m.keyPrefix)
		test.EqOp(t, FailClosed, m.storeFailurePolicy)
	})

	T.Run("rejects a nil store", func(t *testing.T) {
		t.Parallel()

		_, err := NewManager[payload](nil, newLocker(t))
		test.ErrorIs(t, err, ErrNilStore)
	})

	// The locker has no default on purpose: an implicit noop would leave replay
	// working while silently removing mutual exclusion.
	T.Run("rejects a nil locker", func(t *testing.T) {
		t.Parallel()

		_, err := NewManager(newStore(t), nil)
		test.ErrorIs(t, err, ErrNilLocker)
	})

	T.Run("rejects a non-positive TTL", func(t *testing.T) {
		t.Parallel()

		// Options ignore non-positive values, so an invalid TTL has to be set
		// past them to prove the constructor's own guard runs.
		_, err := NewManager(newStore(t), newLocker(t), func(m *Manager[payload]) { m.ttl = 0 })
		test.ErrorIs(t, err, ErrInvalidTTL)

		_, err = NewManager(newStore(t), newLocker(t), func(m *Manager[payload]) { m.inFlightTTL = -time.Second })
		test.ErrorIs(t, err, ErrInvalidTTL)
	})

	T.Run("options override defaults", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t,
			WithTTL[payload](time.Hour),
			WithInFlightTTL[payload](time.Minute),
			WithMaxKeyLength[payload](8),
			WithKeyPrefix[payload]("p:"),
			WithStoreFailurePolicy[payload](FailOpen),
		)

		test.EqOp(t, time.Hour, m.ttl)
		test.EqOp(t, time.Minute, m.inFlightTTL)
		test.EqOp(t, 8, m.maxKeyLength)
		test.EqOp(t, "p:", m.keyPrefix)
		test.EqOp(t, FailOpen, m.storeFailurePolicy)
	})

	T.Run("ignores nil options and zero values", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t, nil, WithTTL[payload](0), WithMaxKeyLength[payload](-1), WithClock[payload](nil))

		test.EqOp(t, DefaultTTL, m.ttl)
		test.EqOp(t, DefaultMaxKeyLength, m.maxKeyLength)
		test.NotNil(t, m.clock)
	})
}

func TestManager_Do_Validation(T *testing.T) {
	T.Parallel()

	fn := func(context.Context) (*payload, error) { return &payload{}, nil }

	T.Run("rejects an empty key", func(t *testing.T) {
		t.Parallel()

		_, err := newTestManager(t).Do(t.Context(), "", testFingerprint, fn)
		test.ErrorIs(t, err, ErrKeyRequired)
	})

	T.Run("rejects an over-long key", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t, WithMaxKeyLength[payload](4))

		_, err := m.Do(t.Context(), "abcde", testFingerprint, fn)
		test.ErrorIs(t, err, ErrKeyTooLong)
	})

	T.Run("rejects a key with disallowed characters", func(t *testing.T) {
		t.Parallel()

		_, err := newTestManager(t).Do(t.Context(), "has space", testFingerprint, fn)
		test.ErrorIs(t, err, ErrKeyInvalid)
	})

	// An empty fingerprint would make every request under a key look identical
	// and disable mismatch detection, so it is refused rather than defaulted.
	T.Run("rejects an empty fingerprint", func(t *testing.T) {
		t.Parallel()

		_, err := newTestManager(t).Do(t.Context(), testKey, "", fn)
		test.ErrorIs(t, err, ErrEmptyFingerprint)
	})

	T.Run("rejects a nil func", func(t *testing.T) {
		t.Parallel()

		_, err := newTestManager(t).Do(t.Context(), testKey, testFingerprint, nil)
		test.ErrorIs(t, err, ErrNilFunc)
	})
}

func TestManager_Do(T *testing.T) {
	T.Parallel()

	T.Run("runs the work and records the result", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		fn := newCountingFn("first")

		res, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)
		test.False(t, res.Replayed)
		test.EqOp(t, "first", res.Value.Name)
		test.EqOp(t, int64(1), fn.Calls())

		record, err := m.store.Get(ctx, m.storeKey(testKey))
		must.NoError(t, err)
		test.EqOp(t, StateCompleted, record.State)
		test.EqOp(t, testFingerprint, record.Fingerprint)
		test.EqOp(t, recordVersion, record.Version)
	})

	T.Run("replays a completed record without running the work", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		fn := newCountingFn("second")

		seed(t, m, testKey, completed(testFingerprint, "first"), time.Hour)

		res, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)
		test.True(t, res.Replayed)
		test.EqOp(t, "first", res.Value.Name)
		test.EqOp(t, int64(0), fn.Calls())
	})

	T.Run("refuses a key already in flight", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t)
		fn := newCountingFn("nope")

		seed(t, m, testKey, inFlight(testFingerprint), time.Hour)

		_, err := m.Do(t.Context(), testKey, testFingerprint, fn.run)
		test.ErrorIs(t, err, ErrInFlight)
		test.EqOp(t, int64(0), fn.Calls())
	})

	T.Run("reports a completed record used with a different fingerprint", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t)
		fn := newCountingFn("nope")

		seed(t, m, testKey, completed("other", "first"), time.Hour)

		_, err := m.Do(t.Context(), testKey, testFingerprint, fn.run)
		test.ErrorIs(t, err, ErrFingerprintMismatch)
		test.EqOp(t, int64(0), fn.Calls())
	})

	// Mismatch beats in-flight. A client reusing one key for two requests has a
	// bug worth surfacing now; telling it to retry is the one answer that
	// cannot help.
	T.Run("reports an in-flight record used with a different fingerprint", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t)
		fn := newCountingFn("nope")

		seed(t, m, testKey, inFlight("other"), time.Hour)

		_, err := m.Do(t.Context(), testKey, testFingerprint, fn.run)
		test.ErrorIs(t, err, ErrFingerprintMismatch)
		test.EqOp(t, int64(0), fn.Calls())
	})

	T.Run("releases the claim when the work errors", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		boom := platformerrors.New("boom")

		fn := newCountingFn("never")
		fn.err = boom

		_, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		test.ErrorIs(t, err, boom)

		_, err = m.store.Get(ctx, m.storeKey(testKey))
		test.ErrorIs(t, err, cache.ErrNotFound)

		// The claim is gone, so the next attempt genuinely re-executes.
		fn.err = nil
		res, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)
		test.False(t, res.Replayed)
		test.EqOp(t, int64(2), fn.Calls())
	})

	T.Run("releases the claim and re-panics when the work panics", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)

		func() {
			defer func() {
				test.NotNil(t, recover())
			}()

			_, _ = m.Do(ctx, testKey, testFingerprint, func(context.Context) (*payload, error) {
				panic("boom")
			})
		}()

		_, err := m.store.Get(ctx, m.storeKey(testKey))
		test.ErrorIs(t, err, cache.ErrNotFound)
	})

	T.Run("releases the claim when the result is not recordable", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t, WithRecordable(func(p *payload) bool { return p.Name != "transient" }))
		fn := newCountingFn("transient")

		res, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)
		test.False(t, res.Replayed)
		test.EqOp(t, "transient", res.Value.Name)

		// The caller still gets the value; it just is not pinned for the TTL.
		_, err = m.store.Get(ctx, m.storeKey(testKey))
		test.ErrorIs(t, err, cache.ErrNotFound)
	})

	// The claim and the result live on different clocks: a claim must expire in
	// minutes so a dead process stops blocking retries, while a result must
	// survive for hours so a late retry still replays. Writing either under the
	// other's expiry is silent and costly, so the resolved durations are
	// asserted rather than the durations the manager was configured with.
	T.Run("writes the claim and the result under their own TTLs", func(t *testing.T) {
		t.Parallel()

		var (
			mu       sync.Mutex
			expiries []time.Duration
		)

		store := newStore(t)
		recording := &cachemock.CacheMock[Record[payload]]{
			GetFunc:    store.Get,
			DeleteFunc: store.Delete,
			SetFunc: func(ctx context.Context, key string, value *Record[payload], opts ...cache.WriteOption) error {
				mu.Lock()
				expiries = append(expiries, cache.EffectiveExpiry(0, opts...))
				mu.Unlock()

				return store.Set(ctx, key, value, opts...)
			},
		}

		m, err := NewManager(recording, newLocker(t),
			WithTTL[payload](12*time.Hour),
			WithInFlightTTL[payload](90*time.Second),
		)
		must.NoError(t, err)

		_, err = m.Do(t.Context(), testKey, testFingerprint, newCountingFn("ttl").run)
		must.NoError(t, err)

		mu.Lock()
		defer mu.Unlock()

		must.SliceLen(t, 2, expiries)
		test.EqOp(t, 90*time.Second, expiries[0])
		test.EqOp(t, 12*time.Hour, expiries[1])
	})
}

func TestManager_Do_DoubleCheck(T *testing.T) {
	T.Parallel()

	// Everything here turns on a record landing between Do's pre-lock read and
	// the claim. The locker mock injects exactly that interleaving, which a
	// timing-based test could not do deterministically.

	T.Run("replays a record that landed while acquiring the lock", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		store := newStore(t)
		fn := newCountingFn("loser")

		var m *Manager[payload]
		locker := grantingLocker(func(ctx context.Context) {
			must.NoError(t, store.Set(ctx, m.storeKey(testKey), completed(testFingerprint, "winner"), cache.WithExpiry(time.Hour)))
		})

		var err error
		m, err = NewManager(store, locker)
		must.NoError(t, err)

		res, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)
		test.True(t, res.Replayed)
		test.EqOp(t, "winner", res.Value.Name)
		test.EqOp(t, int64(0), fn.Calls())
	})

	T.Run("refuses a claim that landed while acquiring the lock", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		store := newStore(t)
		fn := newCountingFn("loser")

		var m *Manager[payload]
		locker := grantingLocker(func(ctx context.Context) {
			must.NoError(t, store.Set(ctx, m.storeKey(testKey), inFlight(testFingerprint), cache.WithExpiry(time.Hour)))
		})

		var err error
		m, err = NewManager(store, locker)
		must.NoError(t, err)

		_, err = m.Do(ctx, testKey, testFingerprint, fn.run)
		test.ErrorIs(t, err, ErrInFlight)
		test.EqOp(t, int64(0), fn.Calls())
	})

	T.Run("propagates a lock failure", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("lock down")
		store := newStore(t)

		m, err := NewManager(store, &lockerStub{err: boom})
		must.NoError(t, err)

		_, err = m.Do(t.Context(), testKey, testFingerprint, newCountingFn("nope").run)
		test.ErrorIs(t, err, boom)
	})
}

func TestManager_Do_ClaimOwnership(T *testing.T) {
	T.Parallel()

	// The claim's owner is the only execution allowed to complete it. Losing
	// ownership means the work outran InFlightTTL and someone else took the
	// key — the one remaining path to a duplicate effect.

	T.Run("skips the write and counts when the claim was taken over", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		lost := &countingCounter{}
		m.claimLostCounter = lost

		_, err := m.Do(ctx, testKey, testFingerprint, func(ctx context.Context) (*payload, error) {
			// Stands in for the claim expiring and another execution claiming
			// the key while this one was still running.
			seed(t, m, testKey, inFlight(testFingerprint), time.Hour)

			return &payload{Name: "late"}, nil
		})
		must.NoError(t, err)

		test.EqOp(t, int64(1), lost.Total())

		// The other owner's claim survives untouched.
		record, err := m.store.Get(ctx, m.storeKey(testKey))
		must.NoError(t, err)
		test.EqOp(t, StateInFlight, record.State)
		test.EqOp(t, "seeded", record.ClaimID)
	})

	T.Run("does not delete another owner's claim on release", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		lost := &countingCounter{}
		m.claimLostCounter = lost

		boom := platformerrors.New("boom")
		_, err := m.Do(ctx, testKey, testFingerprint, func(ctx context.Context) (*payload, error) {
			seed(t, m, testKey, inFlight(testFingerprint), time.Hour)

			return nil, boom
		})
		test.ErrorIs(t, err, boom)
		test.EqOp(t, int64(1), lost.Total())

		record, err := m.store.Get(ctx, m.storeKey(testKey))
		must.NoError(t, err)
		test.EqOp(t, "seeded", record.ClaimID)
	})
}

func TestManager_Do_StoreFailure(T *testing.T) {
	T.Parallel()

	boom := platformerrors.New("redis is down")

	T.Run("fails closed by default", func(t *testing.T) {
		t.Parallel()

		m, err := NewManager(failingStore(t, boom), newLocker(t))
		must.NoError(t, err)

		fn := newCountingFn("nope")
		_, err = m.Do(t.Context(), testKey, testFingerprint, fn.run)

		test.ErrorIs(t, err, ErrStoreUnavailable)
		test.EqOp(t, int64(0), fn.Calls())
	})

	T.Run("fails open when configured to", func(t *testing.T) {
		t.Parallel()

		store := failingStore(t, boom)
		store.SetFunc = func(context.Context, string, *Record[payload], ...cache.WriteOption) error { return nil }
		store.DeleteFunc = func(context.Context, string) error { return nil }

		m, err := NewManager(store, newLocker(t), WithStoreFailurePolicy[payload](FailOpen))
		must.NoError(t, err)

		fn := newCountingFn("ran anyway")
		res, err := m.Do(t.Context(), testKey, testFingerprint, fn.run)

		must.NoError(t, err)
		test.EqOp(t, "ran anyway", res.Value.Name)
		test.EqOp(t, int64(1), fn.Calls())
	})

	T.Run("counts store errors", func(t *testing.T) {
		t.Parallel()

		m, err := NewManager(failingStore(t, boom), newLocker(t))
		must.NoError(t, err)

		errors := &countingCounter{}
		m.storeErrorCounter = errors

		_, _ = m.Do(t.Context(), testKey, testFingerprint, newCountingFn("nope").run)

		test.EqOp(t, int64(1), errors.Total())
	})
}

func TestManager_Do_RecordVersion(T *testing.T) {
	T.Parallel()

	// A day-long TTL means a shape change would otherwise poison every key for
	// a day. Records from another version read as misses instead.
	T.Run("treats a record from another version as a miss", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		stale := &countingCounter{}
		m.staleRecordCounter = stale

		old := completed(testFingerprint, "old")
		old.Version = recordVersion + 1
		seed(t, m, testKey, old, time.Hour)

		fn := newCountingFn("fresh")
		res, err := m.Do(ctx, testKey, testFingerprint, fn.run)

		must.NoError(t, err)
		test.False(t, res.Replayed)
		test.EqOp(t, "fresh", res.Value.Name)
		test.EqOp(t, int64(1), fn.Calls())
		// Once for the pre-lock read, once for the re-read under the lock.
		test.EqOp(t, int64(2), stale.Total())
	})

	T.Run("refuses a record in an unknown state", func(t *testing.T) {
		t.Parallel()

		m := newTestManager(t)

		unknown := completed(testFingerprint, "weird")
		unknown.State = State(200)
		seed(t, m, testKey, unknown, time.Hour)

		fn := newCountingFn("nope")
		_, err := m.Do(t.Context(), testKey, testFingerprint, fn.run)

		test.ErrorIs(t, err, ErrInFlight)
		test.EqOp(t, int64(0), fn.Calls())
	})
}

func TestManager_Do_Outcomes(T *testing.T) {
	T.Parallel()

	T.Run("counts one request per resolved call", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)
		requests := &countingCounter{}
		m.requestCounter = requests

		fn := newCountingFn("v")

		_, err := m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)

		_, err = m.Do(ctx, testKey, testFingerprint, fn.run)
		must.NoError(t, err)

		_, err = m.Do(ctx, testKey, "different", fn.run)
		test.ErrorIs(t, err, ErrFingerprintMismatch)

		// executed + replayed + mismatch.
		test.EqOp(t, int64(3), requests.Total())
	})
}

// TestManager_Do_Concurrent is the test the short-lock design exists to pass:
// many callers, one key, exactly one execution.
func TestManager_Do_Concurrent(T *testing.T) {
	T.Parallel()

	T.Run("runs the work exactly once across concurrent callers", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		m := newTestManager(t)

		const callers = 32

		var (
			executions atomic.Int64
			replays    atomic.Int64
			inFlights  atomic.Int64
			wg         sync.WaitGroup
			release    = make(chan struct{})
		)

		wg.Add(callers)
		for range callers {
			go func() {
				defer wg.Done()

				<-release

				res, err := m.Do(ctx, testKey, testFingerprint, func(context.Context) (*payload, error) {
					executions.Add(1)

					return &payload{Name: "once"}, nil
				})
				switch {
				case err == nil && res.Replayed:
					replays.Add(1)
				case err == nil:
				default:
					test.ErrorIs(t, err, ErrInFlight)
					inFlights.Add(1)
				}
			}()
		}

		close(release)
		wg.Wait()

		test.EqOp(t, int64(1), executions.Load())
		// Everyone else either replayed the winner's result or was correctly
		// refused while the winner was still running.
		test.EqOp(t, int64(callers-1), replays.Load()+inFlights.Load())
	})
}

func TestValidateKey(T *testing.T) {
	T.Parallel()

	T.Run("accepts the key shapes clients actually send", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{
			"d3f1a0c4-5b6e-4a2f-9c8d-1e2f3a4b5c6d", // uuid
			"cv9k2n3c77u4kqp1v2ag",                 // xid
			"abc_-123.XYZ~",                        // base64url-ish
			"a:b",                                  // separators are fine
		} {
			test.NoError(t, ValidateKey(key, DefaultMaxKeyLength))
		}
	})

	T.Run("rejects an empty key", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, ValidateKey("", DefaultMaxKeyLength), ErrKeyRequired)
	})

	T.Run("enforces the maximum length", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, ValidateKey("abcd", 4))
		test.ErrorIs(t, ValidateKey("abcde", 4), ErrKeyTooLong)
	})

	T.Run("a non-positive maximum disables the length check", func(t *testing.T) {
		t.Parallel()

		long := make([]byte, 4096)
		for i := range long {
			long[i] = 'a'
		}

		test.NoError(t, ValidateKey(string(long), 0))
	})

	T.Run("rejects bytes outside printable ASCII", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{
			"has space",
			"tab\there",
			"newline\n",
			"null\x00",
			"unicodé",
			"\x7f",
		} {
			test.ErrorIs(t, ValidateKey(key, DefaultMaxKeyLength), ErrKeyInvalid)
		}
	})
}

func TestKeyContext(T *testing.T) {
	T.Parallel()

	T.Run("round-trips a key", func(t *testing.T) {
		t.Parallel()

		ctx := WithKey(t.Context(), testKey)

		got, ok := KeyFromContext(ctx)
		test.True(t, ok)
		test.EqOp(t, testKey, got)
	})

	T.Run("reports absence", func(t *testing.T) {
		t.Parallel()

		_, ok := KeyFromContext(t.Context())
		test.False(t, ok)
	})

	T.Run("treats an empty key as absent", func(t *testing.T) {
		t.Parallel()

		_, ok := KeyFromContext(WithKey(t.Context(), ""))
		test.False(t, ok)
	})

	T.Run("mints a valid key", func(t *testing.T) {
		t.Parallel()

		ctx, key := WithNewKey(t.Context())

		test.NoError(t, ValidateKey(key, DefaultMaxKeyLength))

		got, ok := KeyFromContext(ctx)
		test.True(t, ok)
		test.EqOp(t, key, got)
	})

	// The point of minting once and reusing the context: every attempt sends
	// the same key. Two separate mints must not.
	T.Run("mints a distinct key per call", func(t *testing.T) {
		t.Parallel()

		_, first := WithNewKey(t.Context())
		_, second := WithNewKey(t.Context())

		test.NotEqOp(t, first, second)
	})
}

// lockerStub fails every acquisition.
type lockerStub struct {
	err error
}

func (l *lockerStub) WithLock(context.Context, string, func(context.Context) error) error {
	return l.err
}

func (l *lockerStub) TryWithLock(context.Context, string, func(context.Context) error) (bool, error) {
	return false, l.err
}
