/*
Package idempotency runs work at most once per client-supplied key.

It exists for the case where a client sends a request, never sees the response,
and retries — and the work in between spent real money. Without a key the
server cannot tell that second request apart from a deliberate second purchase,
so it charges the card twice.

# The client mints the key

This is the part most easily read backwards. The server never issues a key.
The client generates one before its first attempt and reuses that same value on
every retry of the same logical operation:

	ctx, _ := idempotency.WithNewKey(ctx)   // once, OUTSIDE the retry loop

	err := policy.Do(ctx, func(ctx context.Context) error {
		return send(ctx, req)               // every attempt carries the same key
	})

That ordering is the whole contract. A key minted inside the retry loop is a
new key per attempt, which looks like protection and provides none. Nothing on
the server can detect the mistake, because a retry and a deliberate duplicate
are byte-identical.

Because a timed-out request never returns anything, there is deliberately no
round trip to acquire a key. The client already has it.

# What it guarantees

At-most-once *effect*, not exactly-once. Those differ, and the gap is worth
naming:

  - A result that was recorded is replayed instead of re-run.
  - Work that has started and not reported back is refused with ErrInFlight,
    because "did it happen?" is unanswerable and running it again is the worse
    guess.
  - A key reused for a different request is reported with
    ErrFingerprintMismatch rather than answered with the earlier result.

What it cannot promise: work that has its effect and then fails. The charge
landed, the error came back, nothing was recorded, and the retry charges again.
Recording failures instead would be worse — a transient error would be pinned
for the whole TTL and the client could never succeed. See WithRecordable.

# The claim

Do takes a short lock, re-reads, writes an in-flight record, and releases. The
work itself runs outside the lock:

 1. read the record; replay, refuse, or continue
 2. lock -> re-read -> write the claim -> unlock
 3. run the work
 4. record the result, or release the claim

The obvious alternative — hold the lock for the whole execution and let the
lock itself mean "in flight" — is wrong here for four separate reasons, any one
of which is disqualifying:

The postgres ScopedLocker runs its callback inside a database transaction.
Holding the work there means an open transaction per in-flight request: pool
exhaustion, blocked vacuums, replication lag.

The postgres advisory lock folds the key into an int64, so unrelated keys can
collide. Under a held lock a collision answers a legitimate request with a
refusal; under a short one it costs a sub-millisecond wait.

The generic scoped adapter's default TTL is thirty seconds. Any work slower
than that loses mutual exclusion while still running — precisely the failure
this package exists to prevent.

A lock leaves no evidence. When a process is killed mid-execution the lock
evaporates and the retry runs the work again. A record with its own TTL
survives, and the retry is correctly refused until it expires.

# Two records, one owner

Every execution writes twice: a claim, then an outcome. The claim carries a
ClaimID, and only its owner may complete or release it.

That check is not ceremony. If work outruns InFlightTTL, the claim expires,
someone else claims the key, and the original execution finishes into a slot it
no longer owns. Writing anyway would hand the new owner a result from a
different execution. Instead the write is skipped and idempotency_claims_lost
counts it.

That counter is the one to alert on. It is the only remaining path to a
duplicate effect, and it always means the same thing: InFlightTTL is too short
for the work it guards.

# Store failure policy

When the store cannot be read, the two available answers fail in opposite
directions, so the choice belongs to the caller. FailClosed (the default)
refuses the request: a brief outage becomes downtime rather than duplicate
charges. FailOpen runs the work anyway, trading the guarantee for availability.

For anything that moves money, FailClosed is the answer. For a store whose
outage is more expensive than a duplicate, FailOpen exists.

# Choosing TTLs

InFlightTTL is a deadline for the work, not a tuning knob. Set it above the
worst case, not the average — every execution slower than it can produce a
duplicate. Two minutes suits a request-shaped workload.

TTL is how long a client may usefully retry. A day is the common answer and
matches what payment providers publish. Longer costs storage; shorter means a
late retry re-executes.

Endpoints that disagree about that answer do not need a Manager each: Do takes
WithCallTTL, which overrides the retention of one call's record. InFlightTTL has
no per-call equivalent on purpose — it bounds how long a dead process blocks a
retry, which is a property of the deployment rather than of the call.

InFlightTTL is also how long a client is refused after a process dies
mid-execution. Nothing better is possible: with the outcome unknown, refusing
is the conservative answer.

# What T must be

The store is a cache.Cache, and the redis provider serializes with gob. So T
must be a concrete struct with exported fields. An interface-typed field needs
its concrete types registered with gob; `any` does not work at all.

Every record carries a Version. A record written by a different version is
ignored rather than misread, so changing the shape of T is a deploy concern
rather than an outage: in-flight keys from the old shape read as misses. Bump
recordVersion when T changes shape.

A hard decode failure cannot be told apart from a connection failure through
the cache interface, so it follows the store failure policy rather than being
treated as a miss.

The memory provider is for tests. It hands back the live pointer with no
defensive copy — never mutate a record read from the store — and it needs
cache/memory's WithJanitor to reclaim a long TTL, since a key written once and
never read again is never lazily evicted. Redis is the production answer.

# The locker matters

The noop locker acquires unconditionally. With it, replay still works — which
covers the ordinary timeout-then-retry case — but two genuinely concurrent
requests can both claim and both execute. The locker argument is required and
has no default so that nobody arrives there by accident.

# Watching it

	idempotency_claims_lost      the alert. Work outran InFlightTTL and the
	                             claim was taken by someone else.
	idempotency_record_failures  the effect happened, the record did not land,
	                             and a retry will run the work again.
	idempotency_requests         by outcome: executed, replayed, in_flight,
	                             mismatch. The four sum to the request total.
	idempotency_store_errors     store health.
	idempotency_stale_records    records ignored for carrying another version;
	                             expected to spike once after a shape change
	                             and then return to zero.
	idempotency_latency_ms       Do, end to end.

A steady stream of in_flight without matching executed usually means work is
dying mid-execution. mismatch is always a client bug.

# Transports

This package knows nothing about HTTP or gRPC. idempotency/http and
idempotency/grpc adapt it to each, and each ships both halves — the server
middleware or interceptor, and the client transport or interceptor that stamps
the key — so the header name and metadata key are defined once.
*/
package idempotency
