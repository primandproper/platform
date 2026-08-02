// Package postgres implements distributedlock.Locker against PostgreSQL session-
// scoped advisory locks (pg_try_advisory_lock). It uses an existing
// platform/database.Client for connection management.
//
// TTL semantics: PostgreSQL advisory locks have no native TTL, so this provider
// enforces the TTL client-side. Acquire stamps an expiry, Expired reports against
// it, and Refresh both verifies the underlying session is alive and extends the
// expiry by the TTL it is given.
//
// What that does and does not buy you is worth being precise about. The expiry is
// this process's own bookkeeping: it stops a caller from believing it still holds
// a lock it has held past its TTL. It is not enforced by the database, so a
// process that ignores Expired keeps the advisory lock until Release is called or
// the dedicated session ends (a network failure, or Locker.Close). Another process
// will not acquire the lock merely because the first one's TTL lapsed — for a
// hard, server-side bound, use a lease table rather than advisory locks.
//
// Each Acquire reserves a dedicated *sql.Conn from the database client's pool so
// that the matching pg_advisory_unlock targets the same session. The Locker tracks
// outstanding connections internally and releases all of them on Close.
package postgres
