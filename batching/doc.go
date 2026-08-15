/*
Package batching merges concurrent writes against a narrow key space into one
write per process.

It holds two shapes, and the difference between them is who waits:

  - [GroupCommit] is the blocking one. Callers submit items and block until the
    batch carrying them has been written, so read-your-write holds: submit, then
    read, and the row is there. Whatever arrives while a write is in flight rides
    the next one together, so however many callers are writing, exactly one
    statement is ever in flight.

  - [Buffer] is the non-blocking one. Callers add keys and return immediately;
    the keys are deduped into a pending set and flushed on an interval or when
    the set fills. [Buffer.Take] pulls keys back out of the pending set, for a
    caller that has to write them itself first and needs the buffered write not
    to race it.

# The failure this exists to prevent

It is not slowness. A read path that upserts one row per request puts as many
concurrent INSERT … ON CONFLICT DO UPDATE statements against the same handful of
popular rows as it has in-flight requests. Those statements take row locks in
whatever order each caller happened to build them in, deadlock against each
other (Postgres 40P01), and hold a pool connection while they do. The pool
empties, and endpoints with nothing to do with that table start failing.

Merging fixes it at the root: one statement in flight, one entry per key however
many callers named it, and — with [WithMerge] or [WithOrder] — one lock order.
The busier the process gets, the larger the batches become and the fewer
connections the write path holds, which is the opposite of how
one-statement-per-caller behaves under the same load.

# Two things that were learned rather than reasoned

The flush does not inherit a waiter's context. A batch outlives whichever caller
happened to open it, so cancelling one request must not abandon a merged write
that other callers are blocked on. Both types give the flush a context of their
own, bounded by [WithFlushTimeout]; waiters keep their own deadlines and simply
stop waiting, and their items land anyway — which is the right outcome, because
the work was still worth doing.

Ordering belongs to the batcher, not the write function. Lock acquisition on one
total order is what turns contention into a queue instead of a deadlock cycle,
and a write function that receives a map-ordered slice cannot supply that order
however carefully it is written. [WithMerge] emits in key order for exactly this
reason, and [WithOrder] supplies an order for batches that are not keyed.

# What this is not

Not a queue, not a store, and not a retry policy. The write function is the
caller's, and so is what happens when it fails beyond reporting it: GroupCommit
hands the error to that batch's waiters and Buffer logs and counts it, and
neither retries, re-queues, or holds the items back for a second attempt. A
caller that needs durability under a failing write wants a queue — see the
workqueue package, which is itself built on GroupCommit.
*/
package batching
