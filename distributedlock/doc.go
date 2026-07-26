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
// configurable interval. Prefer ScopedLocker unless the hold genuinely must
// outlive a function scope (e.g. a lock held across asynchronous work), where
// the raw Acquire/Release handle remains the right tool.
//
// Provider semantics differ in one important respect: the redis and memory providers
// enforce TTLs natively, while the postgres provider's TTL is advisory only — the
// underlying pg_advisory_lock is held until either Release is called or the
// dedicated session is closed. See distributedlock/postgres for details.
package distributedlock
