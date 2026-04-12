// Package postgres implements distributedlock.Locker against PostgreSQL session-
// scoped advisory locks (pg_try_advisory_lock). It uses an existing
// platform/database.Client for connection management.
//
// IMPORTANT — TTL semantics: PostgreSQL advisory locks have no native TTL. The TTL
// argument to Acquire/Refresh on this provider is ADVISORY ONLY. The lock is held
// until Release is called or the dedicated session is closed (e.g. by a network
// failure or by Locker.Close). Callers that need a hard upper bound on lock
// duration should impose it via context deadlines or by tracking elapsed time
// themselves; the Refresh method on this provider only verifies that the underlying
// session is still alive — it does not extend any expiry on the database side.
//
// Each Acquire reserves a dedicated *sql.Conn from the database client's pool so
// that the matching pg_advisory_unlock targets the same session. The Locker tracks
// outstanding connections internally and releases all of them on Close.
package postgres
