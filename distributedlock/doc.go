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
// Provider semantics differ in one important respect: the redis and memory providers
// enforce TTLs natively, while the postgres provider's TTL is advisory only — the
// underlying pg_advisory_lock is held until either Release is called or the
// dedicated session is closed. See distributedlock/postgres for details.
package distributedlock
