package workqueue

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/primandproper/platform-go/v10/database/dialect"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
)

// enqueueFlushTimeout bounds one merged upsert. It is generous on purpose: the
// batch's waiters have already given up their own deadlines to it, and an
// enqueue that lands late still schedules the work correctly.
const enqueueFlushTimeout = 10 * time.Second

// Entry is one unit of work being offered to the queue.
type Entry[K comparable] struct {
	// Key names the work. It is the row's identity: enqueueing the same key
	// twice updates one row rather than creating two.
	Key K

	// Priority orders the queue ahead of waiting time. Higher goes first, and
	// re-enqueueing an item can only raise it — an enqueue is a claim on
	// attention, so the loudest caller wins and a later, quieter one cannot
	// demote work somebody else already flagged as urgent.
	//
	// It is the generic form of a demand signal. A read path that discovers it
	// needs a key computed sooner enqueues it with a higher priority; that is
	// the whole mechanism, and it is why Enqueue has to be cheap enough to call
	// from a request handler.
	Priority int

	// Delay holds the item back for this long, measured from the database's
	// now() at the moment the row lands.
	//
	// It is a duration rather than a timestamp for the reason the package
	// documentation opens with: an absolute time would be the caller's clock,
	// and the whole point is that no two processes have to agree on one.
	//
	// Re-enqueueing an outstanding item can only move its availability earlier,
	// mirroring Priority. A completed item is being restarted rather than
	// hurried, so it takes the new delay outright.
	Delay time.Duration
}

// Enqueue offers work to the queue, and returns once those keys are durably in
// it.
//
// Every in-flight Enqueue on this process is merged into a single upsert. The
// caller still blocks until its own keys have landed, so read-your-write holds —
// enqueue, then claim, and the key is there — but however many callers are
// enqueueing at once, exactly one statement is ever in flight. That is what
// makes this safe to call from a request handler: the busier the process gets,
// the larger the batches become and the fewer connections the write path holds,
// which is the opposite of how one-statement-per-caller behaves under the same
// load.
//
// A caller whose context expires stops waiting but does not cancel the flush:
// the batch is shared, and its other waiters still need it. Those keys are
// therefore likely to land anyway, which is the right outcome — the work was
// still worth doing.
//
// Keys are validated before they join a batch, so a malformed key fails its own
// Enqueue rather than poisoning everybody else's.
func (q *Queue[K]) Enqueue(ctx context.Context, entries ...Entry[K]) error {
	ctx, op := q.o11y.Begin(ctx, observability.WithValue(itemCountKey, len(entries)))
	defer op.End()

	if len(entries) == 0 {
		return nil
	}

	rows := make([]encodedEntry, 0, len(entries))

	for i := range entries {
		key, err := encodeKey(q.codec, entries[i].Key)
		if err != nil {
			return op.Error(err, "encoding work queue key")
		}

		delay := max(entries[i].Delay, 0)

		rows = append(rows, encodedEntry{
			key:         key,
			priority:    entries[i].Priority,
			delayMicros: delay.Microseconds(),
		})
	}

	if err := q.batcher.enqueue(ctx, rows); err != nil {
		return op.Error(err, "enqueuing work queue items")
	}

	q.enqueuedCounter.Add(ctx, int64(len(entries)), q.attrs)

	return nil
}

// EnqueueKeys is Enqueue for the ordinary case: work with no priority and no
// delay, wanted as soon as a worker is free.
func (q *Queue[K]) EnqueueKeys(ctx context.Context, keys ...K) error {
	entries := make([]Entry[K], 0, len(keys))
	for i := range keys {
		entries = append(entries, Entry[K]{Key: keys[i]})
	}

	return q.Enqueue(ctx, entries...)
}

// upsert writes one merged batch.
func (q *Queue[K]) upsert(ctx context.Context, rows []encodedEntry) error {
	if len(rows) == 0 {
		return nil
	}

	q.enqueueBatchHist.Record(ctx, float64(len(rows)), q.attrs)

	query, args := buildUpsert(q.cfg.resolvedTable(), q.cfg.Name, rows)

	if err := q.retrier.Do(ctx, "enqueue", func() error {
		if _, execErr := q.client.Writer().ExecContext(ctx, query, args...); execErr != nil {
			return platformerrors.Wrap(execErr, "upserting work queue items")
		}

		return nil
	}); err != nil {
		return err
	}

	q.notify(ctx)

	return nil
}

// notify wakes whoever is listening, after the rows are committed and never
// before — a claimer woken early would find nothing and go back to sleep until
// its poll, which is the latency this exists to remove.
//
// A failure here is logged rather than returned. The work is already durably
// enqueued; reporting an error would tell the caller its enqueue failed when it
// did not, and the only consequence of a missing notification is that the item
// waits for a poll — exactly what happens when a listener is reconnecting.
func (q *Queue[K]) notify(ctx context.Context) {
	if q.cfg.NotifyChannel == "" {
		return
	}

	if _, err := q.client.Writer().ExecContext(ctx, dialect.PostgresNotifyStatement, q.cfg.NotifyChannel); err != nil {
		q.o11y.Logger().WithValue(notifyChannelKey, q.cfg.NotifyChannel).Error("notifying work queue channel", err)
	}
}

// enqueueBatcher merges concurrent Enqueue calls into one upsert — group commit,
// the same trick a write-ahead log plays on fsync, for the same reason.
//
// The failure it exists to prevent is not slowness. A read path that enqueues on
// every request issues one multi-row upsert per in-flight request against the
// same handful of popular rows; those upserts take row locks in whatever order
// each caller happened to build, deadlock against each other, and hold a pool
// connection while they do. The pool empties, and endpoints with nothing to do
// with the queue start failing. Merging fixes that at the root: one statement in
// flight, one row per key however many callers named it, and — with the sort in
// buildUpsert — one lock order.
//
// The batcher is deliberately not timer-driven. A flush starts as soon as the
// previous one finishes, so an idle process pays no latency at all and a busy
// one merges more the busier it gets. There is no interval to tune and no
// configuration that can make it wrong.
type enqueueBatcher struct {
	// write is what a flush actually runs. Held as a function rather than a
	// Queue so the batcher's merging can be exercised without a database — it
	// has no other dependency on one.
	write func(ctx context.Context, rows []encodedEntry) error

	// open is the batch now accepting rows; the flusher swaps it out under mu,
	// so a caller that captured it is guaranteed its keys ride that flush.
	open *enqueueBatch

	wake chan struct{}
	stop chan struct{}
	done chan struct{}

	mu       sync.Mutex
	closed   bool
	stopOnce sync.Once
}

// enqueueBatch is one merged group of rows plus the result its waiters read.
// Waiters read err only after done closes, which the flusher does last.
type enqueueBatch struct {
	rows map[string]encodedEntry
	done chan struct{}
	err  error
}

// newEnqueueBatcher starts a batcher that flushes through write.
func newEnqueueBatcher(write func(ctx context.Context, rows []encodedEntry) error) *enqueueBatcher {
	b := &enqueueBatcher{
		write: write,
		wake:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}

	go b.run()

	return b
}

// enqueue adds rows to the batch currently accepting them and blocks until that
// batch has been written.
func (b *enqueueBatcher) enqueue(ctx context.Context, rows []encodedEntry) error {
	batch := b.join(rows)

	select {
	case <-batch.done:
		return batch.err
	case <-ctx.Done():
		return platformerrors.Wrap(ctx.Err(), "waiting for work queue enqueue")
	}
}

// join merges rows into the open batch — creating it if this is the first caller
// — and nudges the flusher. The returned batch is the one those rows will ride,
// captured under the same lock that lets the flusher swap it out.
func (b *enqueueBatcher) join(rows []encodedEntry) *enqueueBatch {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return closedBatch()
	}

	if b.open == nil {
		b.open = &enqueueBatch{
			rows: make(map[string]encodedEntry, len(rows)),
			done: make(chan struct{}),
		}
	}

	for i := range rows {
		b.open.rows[rows[i].key] = mergeEntries(b.open.rows[rows[i].key], rows[i])
	}

	batch := b.open

	select {
	case b.wake <- struct{}{}:
	default: // a flush is already pending; this batch is part of it
	}

	return batch
}

// take swaps the open batch out for flushing. Rows arriving after this point
// start the next batch.
func (b *enqueueBatcher) take() *enqueueBatch {
	b.mu.Lock()
	defer b.mu.Unlock()

	batch := b.open
	b.open = nil

	return batch
}

// run flushes whatever has accumulated, one batch at a time, until close.
func (b *enqueueBatcher) run() {
	defer close(b.done)

	for {
		select {
		case <-b.stop:
			return
		case <-b.wake:
		}

		b.flush(b.take())
	}
}

// flush writes one batch and releases its waiters. Every exit path closes done
// exactly once — a waiter parked on a batch that never completes would hold a
// request open until its own context expired.
//
// The flush gets its own context rather than any caller's, for the same reason:
// the batch is shared, so the first waiter to give up must not cancel the write
// the rest are still waiting on.
func (b *enqueueBatcher) flush(batch *enqueueBatch) {
	if batch == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), enqueueFlushTimeout)
	defer cancel()

	batch.err = b.write(ctx, sortAndMergeRows(batch.rows))
	close(batch.done)
}

// close stops the flusher and writes whatever was still accumulating, so a
// caller blocked in Enqueue during shutdown gets a real answer instead of
// hanging until its context expires. Safe to call more than once.
func (b *enqueueBatcher) close(ctx context.Context) error {
	b.stopOnce.Do(func() { close(b.stop) })

	<-b.done // an in-flight flush finishes before run returns

	b.mu.Lock()
	b.closed = true
	batch := b.open
	b.open = nil
	b.mu.Unlock()

	if batch == nil {
		return nil
	}

	batch.err = b.write(ctx, sortAndMergeRows(batch.rows))
	close(batch.done)

	return batch.err
}

// closedBatch is an already-completed batch carrying the shutdown error.
func closedBatch() *enqueueBatch {
	batch := &enqueueBatch{done: make(chan struct{}), err: ErrClosed}
	close(batch.done)

	return batch
}

// mergeEntries folds a new row into whatever the batch already held for that
// key, applying the same rule the ON CONFLICT clause applies in SQL: at least
// this urgent, at least this soon.
//
// It has to agree with that clause exactly. If merging inside the batch were
// more permissive than merging against the table, two callers naming one key
// would get a different result depending on whether they happened to land in the
// same flush — which is the kind of bug that only appears under load.
func mergeEntries(existing, incoming encodedEntry) encodedEntry {
	if existing.key == "" {
		return incoming
	}

	return encodedEntry{
		key:         existing.key,
		priority:    max(existing.priority, incoming.priority),
		delayMicros: min(existing.delayMicros, incoming.delayMicros),
	}
}

// sortAndMergeRows flattens a batch into the sorted, duplicate-free slice
// buildUpsert requires. The sort is by key, which is the table's primary key
// within a queue, and is the lock ordering the whole design rests on.
func sortAndMergeRows(rows map[string]encodedEntry) []encodedEntry {
	out := make([]encodedEntry, 0, len(rows))
	for key := range rows {
		out = append(out, rows[key])
	}

	slices.SortFunc(out, func(a, b encodedEntry) int {
		return strings.Compare(a.key, b.key)
	})

	return out
}

// sortAndDedupe puts a batch of encoded keys into primary-key order and removes
// repeats, for the writers that bind keys directly.
func sortAndDedupe(keys []string) []string {
	slices.Sort(keys)

	return slices.Compact(keys)
}
