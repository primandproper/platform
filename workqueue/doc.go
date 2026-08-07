/*
Package workqueue is a leased work queue over Postgres: the
SELECT … FOR UPDATE SKIP LOCKED claim/complete/expire pattern, generic over the
key that names a unit of work.

distributedlock scopes job queues out, and rightly — a lock is not a queue. This
is the queue. It is the piece every distributed-systems consumer writes next, and
the piece they usually write badly, because the two things that make it survive
production are not the parts that look hard.

# What it is, and is not

An item is a key and nothing else. There is no payload column: the consumer
already knows how to turn a key into work, and a queue that also stores the work
has to answer questions about encoding, size, and schema evolution that a key
does not raise. If you want to move payloads to a broker, that is outbox.

Scheduling policy stays with the consumer too. This package knows how to hand out
leases fairly and take them back when they lapse; it does not know what "stale"
means for your domain, or which work is urgent. You express both by enqueueing:
Entry.Delay says "not before", Entry.Priority says "ahead of the rest".

# The clock

The database's now() is the only clock. Every timestamp that governs scheduling —
lease expiry, availability, completion, retention — is written and compared
server-side, and no timestamp is ever bound from or returned to a caller's
process. Durations cross the seam instead: a lease is "this many microseconds
from now()", an age comes back as a duration measured against now().

That is why this package has no clock.Clock option, alone among the platform's
scheduling components. Process clocks never have to agree, which is the whole
reason a fleet can coordinate through one table.

# Failure recovery is expiry, and only expiry

There are no fencing tokens and no heartbeats. A worker that dies simply lets its
lease lapse, and the item is handed to somebody else. Nothing detects the death;
nothing has to.

The price of that simplicity is that work must be idempotent. Two workers can
briefly hold the same key — a lease lapses while its holder is merely slow, not
dead — and a straggler's late Complete lands on an item somebody else has already
finished. Both are waste, not corruption, as long as doing the work twice is the
same as doing it once. Item.Reclaimed marks a claim that took over a lapsed
lease, so the duplicate window is at least visible.

# The two details that cost real incidents

Both of these came out of a production system running this pattern, and neither
is obvious until it bites. They are the reason this package exists rather than
the twenty lines of SQL underneath it.

*Every writer takes its row locks in primary-key order.* Enqueue sorts its rows
before building the statement, and Complete, Release, and Remove reach their rows
through a CTE that orders and locks them explicitly. With one total order,
contention between concurrent batch writers degrades into a queue; without it,
two batches that overlap in opposite orders deadlock (SQLSTATE 40P01) the moment
they meet. Claim is exempt and safe: SKIP LOCKED never waits, and a writer that
never waits cannot be in a lock cycle.

*Enqueue group-commits.* One statement per caller does not survive contact with a
read path. Every in-flight Enqueue on a process is merged into a single upsert,
so however many callers are enqueueing, exactly one statement is ever in flight —
and overlapping key sets collapse into one row apiece instead of contending for
the same row. Callers still block until their own keys have landed, so
read-your-write holds: enqueue, then claim, and the key you enqueued is there.
The unmerged version of this once wedged a service by parking thirty pool
connections in deadlocking upserts, starving every unrelated endpoint of a
connection.

The batcher owns a goroutine, which is why a Queue has to be Closed.

# Driving it

Claim, work, Complete. Release hands work back early with a delay and a reason;
otherwise the lease lapses on its own and the item returns anyway.

	items, err := queue.Claim(ctx, 100, 30*time.Second)
	// ...
	for _, item := range items {
	    if err := do(ctx, item.Key); err != nil {
	        _ = queue.Release(ctx, time.Minute, err, item.Key)

	        continue
	    }

	    done = append(done, item.Key)
	}

	err = queue.Complete(ctx, done...)

A claim's limit counts the items it actually leased, not the rows it looked at:
Postgres applies the LIMIT above the lock, so rows a concurrent claimer holds are
skipped and replaced rather than subtracted. A fleet of claimers all get full
batches while work remains, and a short batch means the queue really is nearly
drained. That depends on the shape of the claim statement — a LIMIT pushed into a
subquery below the lock would silently start returning short batches — so there
is a test pinning it.

Reap and Stats are methods rather than a loop this package runs, because you
already have a scheduler — see the jobs package. Reap deletes completed items
past their retention; Stats is the health read. Nothing here fails loudly, so
Stats.OldestReadyAge is the number that tells you the fleet has stopped draining:
depth alone cannot distinguish a queue that is deep and moving from one that is
deep and stuck.

# Keys

K is comparable, which is most of what makes an encoding safe: maps and slices
are already excluded, so the JSON rendering of a struct key is stable across
processes and releases as long as its field order is. Strings and string-like
types are stored as themselves rather than JSON-quoted, so the table stays
legible. Anything else — a key that has to sort a particular way, or that already
has a canonical string form — supplies WithKeyCodec.

The encoded key is the table's primary key and is bounded by MaxKeyLength; an
over-long key is rejected at Enqueue rather than silently truncated.

# Creating the table

workqueue/migrations renders the DDL for a table prefix. If you already run
database/migrate, hand migrations.SQL to WithGeneratedMigration and the table is
created by your normal migration run at a version you choose.

One table serves any number of logical queues: Config.Name partitions it, and is
the leading column of the primary key. Two Queue values with different names
share nothing but storage.

# Postgres only

Deliberately. The contract above is "the database's now() is the only clock, and
SKIP LOCKED is the arbiter", and the SQL that delivers it — a lock-ordering CTE,
a single-statement claim with RETURNING, interval arithmetic on the server — is
written against Postgres rather than reduced to a portable subset.

SKIP LOCKED is not the part that binds. MySQL 8.0 has it, and CTEs too; what it
has no form of is RETURNING. The claim is one statement that selects due rows,
locks them, increments attempts, extends the lease, and hands back the keys, and
without RETURNING those become a SELECT … FOR UPDATE SKIP LOCKED and a separate
UPDATE inside a transaction held across both round trips. That is a different
concurrency shape with a different failure model — a second implementation
rather than a dialect switch. SQLite is a harder no: it is single-writer, with no
row-level locking to skip.

So New returns dialect.ErrUnsupported for anything but Postgres, rather than
degrading to a lease-only claim that would look like it worked. If a second
backend is ever wanted, the shape to reach for is this package as the interface
with a workqueue/postgres beneath it, the way cache and cache/redis sit — nothing
here forecloses that.
*/
package workqueue
