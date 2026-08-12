/*
Package memory implements distributedlock.Locker over a map and a mutex.

# It is not distributed

The name of the parent package is about the interface, not about this
implementation. Mutual exclusion here extends exactly as far as one *Locker
value: two replicas each hold their own map, so both acquire the same key at the
same moment and neither learns of the other. Nothing detects that, and nothing
reports it — the second holder's Acquire succeeds, and whatever the lock was
protecting runs twice.

Two Lockers built in the same process do not exclude each other either, for the
same reason. The lock's scope is the value, not the key.

So it is the right choice for tests, for a single-replica deployment, and as a
readable statement of the semantics the other providers implement. It is the
wrong choice anywhere the answer to "what happens if this runs twice" is not
"nothing" — a scheduled job, a leader election, an exactly-once batch. Those want
the redis or postgres provider, where the state lives outside any one process.

# Semantics

TTLs are enforced by this process's clock, and lazily: an expired key is
reclaimed when it is acquired again, and by an opportunistic sweep on each
Acquire. Release and Refresh check the ownership token as well as the expiry, so
a caller cannot release a key whose lock has already lapsed and been taken by
someone else — that reports distributedlock.ErrLockNotHeld.

Nothing here fails from unavailability: Ping always succeeds, there is no circuit
breaker, and no network call can time out mid-hold. Close drops every held lock
at once, after which outstanding handles report ErrLockNotHeld.
*/
package memory
