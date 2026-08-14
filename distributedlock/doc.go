// Package distributedlock provides a pessimistic mutual-exclusion atom for
// coordinating exclusive access to a named resource across processes. Provider
// implementations live in subpackages and are selected at runtime via
// distributedlock/config.
//
// The interface is intentionally narrow: Acquire/Release/Refresh, with no built-in
// retry loop or queueing. Callers compose Acquire with platform/retry, the platform
// circuit breaker, or their own backoff strategy. Higher-level concerns such as
// leader election, distributed cron, and exactly-once batch execution are
// compositions on top of this atom and live in consuming applications, not in
// platform.
//
// For the common run-fn-while-held shape, ScopedLocker (WithLock/TryWithLock)
// removes the handle entirely: the lock is released when fn returns, panics
// included. The postgres provider implements it natively with
// transaction-scoped advisory locks — waiters queue server-side, a crashed
// holder's lock dies with its connection, and no session or connection is
// pinned beyond fn's duration. Any other Locker gains the same surface through
// the NewScopedLocker adapter, which polls a contended WithLock on a
// configurable interval that backs off exponentially and is jittered, so a
// crowd of waiters on one key does not hammer the underlying store. Prefer
// ScopedLocker unless the hold genuinely must
// outlive a function scope (e.g. a lock held across asynchronous work), where
// the raw Acquire/Release handle remains the right tool.
//
// Both ScopedLocker implementations emit the same shape of telemetry, so a
// dashboard survives a provider swap: a span covering the whole
// acquire-run-release operation, plus acquire, contention, and error counters
// and a latency histogram. Contention is counted once per call in both, even
// though the generic adapter reaches it by polling and postgres by waiting
// server-side; the adapter additionally reports how long it waited
// (scoped_lock_wait_ms) and how many polls that took. Watch
// scoped_lock_release_failures in particular: it means a lock's TTL elapsed
// while fn was still running, so mutual exclusion was not actually held for
// the whole call.
//
// Provider semantics differ in one important respect: the redis and memory providers
// enforce TTLs natively, while the postgres provider's TTL is advisory only — the
// underlying pg_advisory_lock is held until either Release is called or the
// dedicated session is closed. See distributedlock/postgres for details.
//
// Everything else about a Locker and a ScopedLocker is meant to be the same
// answer whichever provider is configured, and distributedlock/distributedlocktest
// is where that is written down and enforced: one suite, run against every
// implementation here, and available to anyone writing another. A provider
// that departs from the contract — the advisory TTL above is the only one that
// does today — says so at its call to the suite rather than in prose.
package distributedlock
