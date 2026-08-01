package metering

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/primandproper/platform-go/v9/capitalism"
	capitalismnoop "github.com/primandproper/platform-go/v9/capitalism/noop"
	platformerrors "github.com/primandproper/platform-go/v9/errors"
	"github.com/primandproper/platform-go/v9/jobs"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// flusherEnv is one flusher with the pieces a test needs to reach around it.
type flusherEnv struct {
	flusher  *Flusher
	store    Store
	reporter *recordingReporter
	clock    *stubClock
}

// newTestFlusher builds a flusher over a fresh store and a recording reporter.
func newTestFlusher(t *testing.T, mapper ProviderMapper, opts ...FlusherOption) *flusherEnv {
	t.Helper()

	env := newSQLiteEnv(t)
	store := env.newStore(t)

	return newTestFlusherOver(t, store, mapper, opts...)
}

// newTestFlusherOver is newTestFlusher over a store the caller supplies, for the
// tests that need a partially broken one.
func newTestFlusherOver(t *testing.T, store Store, mapper ProviderMapper, opts ...FlusherOption) *flusherEnv {
	t.Helper()

	c := newStubClock()
	reporter := &recordingReporter{}

	flusher, err := NewFlusher(t.Context(), &FlusherConfig{}, store, mapper, reporter,
		append([]FlusherOption{WithFlusherClock(c)}, opts...)...)
	must.NoError(t, err)

	return &flusherEnv{flusher: flusher, store: store, reporter: reporter, clock: c}
}

func TestNewFlusher(T *testing.T) {
	T.Parallel()

	store := newSQLiteEnv(T).newStore(T)
	mapper := staticMapper("si_123")

	T.Run("refuses a nil config, store, mapper, or reporter", func(t *testing.T) {
		t.Parallel()

		_, err := NewFlusher(t.Context(), nil, store, mapper, &recordingReporter{})
		test.Error(t, err)

		_, err = NewFlusher(t.Context(), &FlusherConfig{}, nil, mapper, &recordingReporter{})
		test.ErrorIs(t, err, ErrNilStore)

		_, err = NewFlusher(t.Context(), &FlusherConfig{}, store, nil, &recordingReporter{})
		test.ErrorIs(t, err, ErrNilProviderMapper)

		// No implicit noop: a flusher that silently posted nowhere would mark
		// usage flushed and advance the sequence, so a month of revenue would be
		// discarded by an omission in the wiring.
		_, err = NewFlusher(t.Context(), &FlusherConfig{}, store, mapper, nil)
		test.ErrorIs(t, err, ErrNilUsageReporter)
	})

	T.Run("accepts the explicit noop reporter", func(t *testing.T) {
		t.Parallel()

		// "Meter everything, bill nothing" is a supported deployment; it just has
		// to be said out loud at the call site.
		flusher, err := NewFlusher(t.Context(), &FlusherConfig{}, store, mapper,
			capitalismnoop.NewUsageReporter())
		must.NoError(t, err)
		must.NotNil(t, flusher)
	})

	T.Run("fills defaults and ignores nil options", func(t *testing.T) {
		t.Parallel()

		cfg := &FlusherConfig{}

		flusher, err := NewFlusher(t.Context(), cfg, store, mapper, &recordingReporter{}, nil,
			WithFlusherClock(nil), WithFlusherLogger(nil),
			WithFlusherTracerProvider(nil), WithFlusherMetricsProvider(nil))
		must.NoError(t, err)

		test.EqOp(t, DefaultFlushBatchSize, flusher.cfg.BatchSize)
		test.EqOp(t, DefaultMaxFlushAttempts, flusher.cfg.MaxAttempts)
		test.NotNil(t, flusher.clock)
	})

	T.Run("refuses a lease that cannot cover a post", func(t *testing.T) {
		t.Parallel()

		// A lease that expires while the post it covers is in flight is not a
		// lease, and two flushers posting the same total concurrently is the
		// duplicate charge no idempotency key can undo.
		_, err := NewFlusher(t.Context(), &FlusherConfig{
			FlushTimeout:  time.Minute,
			LeaseDuration: time.Second,
		}, store, mapper, &recordingReporter{})

		test.Error(t, err)
	})
}

func TestFlusher_Flush(T *testing.T) {
	T.Parallel()

	T.Run("posts the accumulated usage", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Claimed)
		test.EqOp(t, 1, result.Flushed)
		test.EqOp(t, int64(42), result.Quantity)

		posts := env.reporter.recorded()
		must.SliceLen(t, 1, posts)

		test.EqOp(t, "si_123", posts[0].SubscriptionItemID)
		test.EqOp(t, int64(42), posts[0].Quantity)
		test.StrHasPrefix(t, idempotencyKeyPrefix, posts[0].IdempotencyKey)
		test.EqOp(t, testMeter, posts[0].Metadata["metering_meter"])
		test.EqOp(t, testSubject, posts[0].Metadata["metering_subject"])
		test.EqOp(t, "0", posts[0].Metadata["metering_sequence"])
	})

	T.Run("posts the delta, not the running total", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))
		_, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		must.NoError(t, mustRecord(t, env.store, newEntry("req-2", 8, AggregationSum)))
		_, err = env.flusher.Flush(t.Context())
		must.NoError(t, err)

		posts := env.reporter.recorded()
		must.SliceLen(t, 2, posts)

		// Providers aggregate the records inside a billing period. Posting the
		// running total every flush would invoice the sum of every partial total
		// ever posted.
		test.EqOp(t, int64(42), posts[0].Quantity)
		test.EqOp(t, int64(8), posts[1].Quantity)
		// A fresh sequence, so the provider accepts it rather than deduplicating
		// it against the first.
		test.NotEqOp(t, posts[0].IdempotencyKey, posts[1].IdempotencyKey)
		test.EqOp(t, "1", posts[1].Metadata["metering_sequence"])
	})

	T.Run("posts nothing when there is nothing to post", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 0, result.Claimed)
		test.SliceEmpty(t, env.reporter.recorded())
	})

	T.Run("posts nothing twice for the same usage", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		_, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		second, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 0, second.Claimed)
		test.SliceLen(t, 1, env.reporter.recorded())
	})

	T.Run("settles without posting when the subject does not bill for the meter", func(t *testing.T) {
		t.Parallel()

		// A free plan on a metered endpoint. Not a failure — and settled rather
		// than retried, so it does not become the permanent head of the queue.
		env := newTestFlusher(t, ProviderMapperFunc(
			func(context.Context, string, string) (ProviderRef, error) {
				return ProviderRef{}, platformerrors.Wrap(ErrNoProviderRef, "free plan")
			}))

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Skipped)
		test.EqOp(t, 0, result.Flushed)
		test.SliceEmpty(t, env.reporter.recorded())

		// Settled, so it is not claimed again.
		again, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)
		test.EqOp(t, 0, again.Claimed)
	})

	T.Run("settles without posting for an empty provider handle", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper(""))

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Skipped)
		test.SliceEmpty(t, env.reporter.recorded())
	})

	T.Run("retries a total whose mapping failed", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, ProviderMapperFunc(
			func(context.Context, string, string) (ProviderRef, error) {
				return ProviderRef{}, errArbitrary
			}))

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Failed)
		test.EqOp(t, 0, result.Flushed)

		// A mapping failure is transient — a lookup service being down — so it
		// comes back, unlike ErrNoProviderRef.
		env.clock.advance(time.Hour)

		later, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)
		test.EqOp(t, 1, later.Claimed)
	})

	T.Run("retries a post the provider refused, under the same key", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))
		env.reporter.err = errArbitrary

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)
		test.EqOp(t, 1, result.Failed)

		env.reporter.err = nil
		env.clock.advance(time.Hour)

		retried, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)
		test.EqOp(t, 1, retried.Flushed)

		posts := env.reporter.recorded()
		must.SliceLen(t, 1, posts)

		// The sequence did not move on the failure, so the retry carries the same
		// delta under the same key — and the provider deduplicates it if the
		// first attempt actually landed and failed on the way back.
		test.EqOp(t, "0", posts[0].Metadata["metering_sequence"])
	})

	T.Run("contains a panic from the provider SDK", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))
		env.reporter.panicNow = true

		must.NoError(t, mustRecord(t, env.store, newEntry("req-1", 42, AggregationSum)))

		// A provider SDK is third-party code on the money path. A panic there
		// would otherwise take the goroutine and every other total in the batch
		// with it.
		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Failed)
	})

	T.Run("gives up after exhausting attempts, without discarding the usage", func(t *testing.T) {
		t.Parallel()

		store := newSQLiteEnv(t).newStore(t)
		env := newTestFlusherOver(t, store, staticMapper("si_123"))
		env.flusher.cfg.MaxAttempts = 2
		env.reporter.err = errArbitrary

		must.NoError(t, mustRecord(t, store, newEntry("req-1", 42, AggregationSum)))

		for range 3 {
			_, err := env.flusher.Flush(t.Context())
			must.NoError(t, err)

			env.clock.advance(time.Hour)
		}

		// Left where it is rather than marked flushed. Marking it would discard
		// usage nobody has been billed for; leaving it keeps the row visible and
		// the money recoverable by hand.
		total, err := store.Total(t.Context(), testSubject, testMeter, monthBounds)
		must.NoError(t, err)

		test.True(t, total.Pending())
		test.EqOp(t, int64(0), total.FlushedQuantity)

		// And it stops costing a provider call every interval.
		exhausted, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)
		test.EqOp(t, 0, exhausted.Claimed)
	})

	T.Run("reports a claim failure", func(t *testing.T) {
		t.Parallel()

		store := newSQLiteEnv(t).newStore(t)
		env := newTestFlusherOver(t, &failingClaimStore{Store: store}, staticMapper("si_123"))

		_, err := env.flusher.Flush(t.Context())

		test.ErrorIs(t, err, errArbitrary)
	})

	T.Run("reaps even when the posts failed", func(t *testing.T) {
		t.Parallel()

		store, prefix := newSQLiteEnv(t).newStoreWithPrefix(t)
		env := newTestFlusherOver(t, store, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, store, newEntry("settled", 10, AggregationSum)))
		_, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		// Now break the provider and add more usage. The reap is an unrelated
		// chore sharing a schedule, and a provider being unreachable is not a
		// reason to let the event table grow unbounded.
		env.reporter.err = errArbitrary
		must.NoError(t, mustRecord(t, store, newEntry("unsettled", 10, AggregationSum)))

		env.clock.advance(DefaultEventRetention + time.Hour)

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Failed)
		// Neither event is reaped: the period now owes the provider again, and
		// the reap's own predicate refuses to touch anything a failed post still
		// needs.
		test.EqOp(t, int64(0), result.EventsReaped)
		test.EqOp(t, 2, countRows(t, newSQLiteEnvFor(t, store), prefix+"_metering_events"))
	})

	T.Run("reaps settled events", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)
		store, prefix := env.newStoreWithPrefix(t)
		flushEnv := newTestFlusherOver(t, store, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, store, newEntry("req-1", 42, AggregationSum)))

		_, err := flushEnv.flusher.Flush(t.Context())
		must.NoError(t, err)

		flushEnv.clock.advance(DefaultEventRetention + time.Hour)

		result, err := flushEnv.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, int64(1), result.EventsReaped)
		test.EqOp(t, 0, countRows(t, env, prefix+"_metering_events"))
	})

	T.Run("skips the reap when disabled", func(t *testing.T) {
		t.Parallel()

		env := newSQLiteEnv(t)
		store, prefix := env.newStoreWithPrefix(t)
		flushEnv := newTestFlusherOver(t, store, staticMapper("si_123"))
		flushEnv.flusher.cfg.DisableReap = true

		must.NoError(t, mustRecord(t, store, newEntry("req-1", 42, AggregationSum)))

		_, err := flushEnv.flusher.Flush(t.Context())
		must.NoError(t, err)

		flushEnv.clock.advance(DefaultEventRetention + time.Hour)

		result, err := flushEnv.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, int64(0), result.EventsReaped)
		test.EqOp(t, 1, countRows(t, env, prefix+"_metering_events"))
	})

	T.Run("reports a reap failure without losing the flush result", func(t *testing.T) {
		t.Parallel()

		store := newSQLiteEnv(t).newStore(t)
		env := newTestFlusherOver(t, &failingReapStore{Store: store}, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())

		test.ErrorIs(t, err, errArbitrary)
		// The posts still happened and are still reported, because the two are
		// unrelated chores.
		must.NotNil(t, result)
		test.EqOp(t, 1, result.Flushed)
	})

	T.Run("survives a settle that fails after the provider has the usage", func(t *testing.T) {
		t.Parallel()

		store := newSQLiteEnv(t).newStore(t)
		env := newTestFlusherOver(t, &failingSettleStore{Store: store}, staticMapper("si_123"))

		must.NoError(t, mustRecord(t, store, newEntry("req-1", 42, AggregationSum)))

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		// The provider has it and the row does not say so. The next pass posts
		// the same delta under the same sequence and the provider deduplicates it
		// — which is the whole reason the key is derived from the sequence.
		test.EqOp(t, 1, result.Failed)
		test.SliceLen(t, 1, env.reporter.recorded())
	})

	T.Run("survives a release that fails", func(t *testing.T) {
		t.Parallel()

		store := newSQLiteEnv(t).newStore(t)
		env := newTestFlusherOver(t, &failingReleaseStore{Store: store}, staticMapper("si_123"))
		env.reporter.err = errArbitrary

		must.NoError(t, mustRecord(t, store, newEntry("req-1", 42, AggregationSum)))

		// The lease simply expires instead. Slower than an explicit release, and
		// the total is picked up again either way.
		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 1, result.Failed)
	})

	T.Run("posts several totals concurrently", func(t *testing.T) {
		t.Parallel()

		env := newTestFlusher(t, staticMapper("si_123"))

		for _, subject := range []string{"a", "b", "c", "d", "e"} {
			entry := newEntry("req-"+subject, 1, AggregationSum)
			entry.Subject = subject

			must.NoError(t, mustRecord(t, env.store, entry))
		}

		result, err := env.flusher.Flush(t.Context())
		must.NoError(t, err)

		test.EqOp(t, 5, result.Flushed)
		test.SliceLen(t, 5, env.reporter.recorded())
	})
}

func TestFlusher_reportTimestamp(T *testing.T) {
	T.Parallel()

	env := newTestFlusher(T, staticMapper("si_123"))

	T.Run("stamps now while the period is open", func(t *testing.T) {
		t.Parallel()

		// A provider rejects a usage record dated ahead of now, so a period still
		// running is stamped now rather than at its end.
		stamped := env.flusher.reportTimestamp(&Total{PeriodEnd: monthBounds.End})

		test.EqOp(t, baseTime, stamped)
	})

	T.Run("stamps inside a closed period", func(t *testing.T) {
		t.Parallel()

		// A record dated after the period has closed lands on the next invoice,
		// which is the wrong one.
		closed := baseTime.Add(-time.Hour)
		stamped := env.flusher.reportTimestamp(&Total{PeriodEnd: closed})

		test.EqOp(t, closed.Add(-time.Second), stamped)
	})
}

func TestFlushIdempotencyKey(T *testing.T) {
	T.Parallel()

	total := &Total{
		Subject: testSubject, Meter: testMeter,
		PeriodStart: monthBounds.Start, FlushSequence: 0,
	}

	T.Run("is stable for the same post", func(t *testing.T) {
		t.Parallel()

		// Exactly as stable as the post it identifies: a retry computes the same
		// key and the provider ignores the duplicate.
		test.EqOp(t, FlushIdempotencyKey(total), FlushIdempotencyKey(total))
	})

	T.Run("varies with the sequence", func(t *testing.T) {
		t.Parallel()

		next := *total
		next.FlushSequence = 1

		test.NotEqOp(t, FlushIdempotencyKey(total), FlushIdempotencyKey(&next))
	})

	T.Run("varies with the subject, meter, and period", func(t *testing.T) {
		t.Parallel()

		base := FlushIdempotencyKey(total)

		otherSubject := *total
		otherSubject.Subject = "account-2"
		test.NotEqOp(t, base, FlushIdempotencyKey(&otherSubject))

		otherMeter := *total
		otherMeter.Meter = "llm_tokens"
		test.NotEqOp(t, base, FlushIdempotencyKey(&otherMeter))

		otherPeriod := *total
		otherPeriod.PeriodStart = monthBounds.End
		test.NotEqOp(t, base, FlushIdempotencyKey(&otherPeriod))
	})

	T.Run("fits a provider's key limit whatever the subject is", func(t *testing.T) {
		t.Parallel()

		// Hashed rather than concatenated: a subject ID is an application's own
		// identifier and may be long or non-ASCII, and a key truncated at 255
		// bytes would collide with a different subject's.
		long := *total
		long.Subject = strings.Repeat("very-long-account-identifier-", 40)

		key := FlushIdempotencyKey(&long)

		test.Less(t, MaxIdempotencyKeyLength, len(key))
		test.StrHasPrefix(t, idempotencyKeyPrefix, key)
	})

	T.Run("handles a nil total", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", FlushIdempotencyKey(nil))
	})
}

func TestFlusher_Job(T *testing.T) {
	T.Parallel()

	env := newTestFlusher(T, staticMapper("si_123"))

	job := env.flusher.Job(jobs.MustCron("0 * * * *"), 10*time.Minute)

	// The name is a constant because it is the job's lock key: two replicas that
	// disagree about it both run the flush, and the flush spends money.
	test.EqOp(T, DefaultFlushJobName, job.Name)
	test.EqOp(T, 10*time.Minute, job.LeaseTTL)
	must.NotNil(T, job.Run)

	must.NoError(T, mustRecord(T, env.store, newEntry("req-1", 5, AggregationSum)))
	must.NoError(T, job.Run(T.Context()))

	test.SliceLen(T, 1, env.reporter.recorded())
}

func TestFlusher_backoff(T *testing.T) {
	T.Parallel()

	env := newTestFlusher(T, staticMapper("si_123"))

	// Full jitter rather than the equal jitter retry.Execute sleeps with: this
	// schedule is written into a row and read by a fleet, and without spreading, a
	// provider outage synchronizes every replica's retries onto one instant.
	for _, attempts := range []int{0, 1, 5, 100} {
		delay := env.flusher.backoff(attempts)

		test.Greater(T, time.Duration(0), delay, test.Sprintf("attempts %d", attempts))
		test.LessEq(T, env.flusher.cfg.Backoff.MaxDelay, delay, test.Sprintf("attempts %d", attempts))
	}
}

func TestTruncateError(T *testing.T) {
	T.Parallel()

	test.EqOp(T, "", truncateError(nil))
	test.EqOp(T, "boom", truncateError(platformerrors.New("boom")))

	T.Run("bounds a long rendering", func(t *testing.T) {
		t.Parallel()

		// A provider error can carry the request body back, and the request body
		// is a customer's usage.
		long := truncateError(platformerrors.New(strings.Repeat("x", maxStoredErrorLength*2)))

		test.EqOp(t, maxStoredErrorLength, len(long))
	})

	T.Run("cuts on a rune boundary", func(t *testing.T) {
		t.Parallel()

		// Half a multi-byte rune is invalid UTF-8, which some JSON encoders
		// refuse and others silently replace.
		rendered := truncateError(platformerrors.New(strings.Repeat("é", maxStoredErrorLength)))

		test.True(t, len(rendered) <= maxStoredErrorLength)
		test.True(t, strings.ToValidUTF8(rendered, "") == rendered)
	})
}

// newSQLiteEnvFor rebuilds a storeEnv around an existing store's client, for the
// tests that need to count rows in tables a helper created.
func newSQLiteEnvFor(t *testing.T, store Store) *storeEnv {
	t.Helper()

	s, ok := store.(*sqlStore)
	must.True(t, ok)

	return &storeEnv{client: s.client, dialect: s.dialect}
}

// usageReporterIsSatisfied keeps the noop's interface conformance checked at
// compile time.
var _ capitalism.UsageReporter = (*recordingReporter)(nil)
