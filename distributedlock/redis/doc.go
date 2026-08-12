/*
Package redis implements distributedlock.Locker with a single Redis key per
lock.

# What it guarantees

Acquire is a SETNX of a prefixed key to a freshly generated ownership token,
with the TTL as the key's expiry — so the TTL is enforced by Redis rather than by
the holder, and a process that dies mid-hold releases the lock when the key
expires rather than when somebody notices.

Release and Refresh are Lua scripts that compare the token before acting, which
is what stops one caller releasing or extending a lock that has already expired
and been taken by another. Either reports distributedlock.ErrLockNotHeld when the
token no longer matches. A held lock whose TTL lapses is indistinguishable, from
inside, from one still held: the handle's TTL is what was asked for, not what
remains, so work that may outlive its TTL has to Refresh.

# What it does not guarantee

Exclusivity rests on that one key, on one Redis. This is not Redlock — nothing
here takes a quorum across independent nodes — so the lock is only as durable as
the key. Redis replication is asynchronous, so a failover that promotes a replica
which has not yet received the SET can hand the same lock to a second caller, and
neither caller is told. Where two simultaneous holders would be a correctness
failure rather than a wasted duplicate, the lock is not the last line of defense;
the work underneath it also needs to be idempotent, or fenced by something the
database enforces.

Contention and unavailability are different answers. A key already held reports
ErrLockNotAcquired and counts as a healthy round trip — the backend answered.
A Redis that cannot be reached trips the circuit breaker, and calls made while it
is open report circuitbreaking.ErrCircuitBroken rather than a failure to acquire,
so a caller cannot read an outage as contention.

There is no waiting. Acquire either takes the lock or returns immediately; a
caller that wants to queue composes retry itself, or uses the parent package's
ScopedLocker, which does that polling for it.

Keys are prefixed (default "lock:"), so what is written to Redis is not the key
the caller named. Spans carry both.
*/
package redis
