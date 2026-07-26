package postgres

// The advisory-lock statements this package issues, collected so the exact SQL
// is reviewable in one place rather than inline at each call site.
//
// Session-scoped (queryAcquireSession, queryReleaseSession) and
// transaction-scoped (queryLockXact, queryTryLockXact) locks are different
// Postgres primitives and are not interchangeable: a session lock outlives its
// transaction and must be released explicitly, while an xact lock is released
// by the server when the transaction ends. Each takes the lock ID as $1.
const (
	// queryAcquireSession takes a session-level advisory lock without waiting,
	// returning whether it was granted. The matching release must run on the
	// same connection.
	queryAcquireSession = `SELECT pg_try_advisory_lock($1)`

	// queryReleaseSession releases a session-level advisory lock, returning
	// false when the current session did not hold it.
	queryReleaseSession = `SELECT pg_advisory_unlock($1)`

	// queryLockXact takes a transaction-scoped advisory lock, blocking
	// server-side until it is granted. It is released with the transaction.
	queryLockXact = `SELECT pg_advisory_xact_lock($1)`

	// queryTryLockXact takes a transaction-scoped advisory lock without
	// waiting, returning whether it was granted.
	queryTryLockXact = `SELECT pg_try_advisory_xact_lock($1)`

	// queryPing is the liveness probe for a held connection.
	queryPing = `SELECT 1`
)
