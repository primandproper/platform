/*
Package sessions keeps session state on the server and gives the client only an
identifier.

That division is the whole point. A cookie carrying the state itself has to be
signed, has to be encrypted if any of it is private, cannot be revoked before it
expires, and grows with whatever anyone thought to put in it. A cookie carrying
a 256-bit random identifier can be revoked by deleting one row, tells an
attacker who reads it nothing, and never grows.

	store, _ := sessions.NewStore(backend, sessions.WithIdleTimeout(30*time.Minute))

	session, _ := store.New(ctx, &Principal{UserID: "u_123"})
	// hand session.ID to the client; sessions/http puts it in a signed cookie

	session, err := store.Get(ctx, id)   // ErrNotFound / ErrExpired if it is over

# The three layers

A Store is what callers hold: identifiers in, payloads out, expiry enforced. A
Backend is where the records physically live — sessions/cache over any
cache.Cache, sessions/database over SQL. sessions/http binds a Store to a signed
cookie and to net/http.

The split between Store and Backend is not ceremony. The parts of a session
store that are easy to get subtly wrong are the same parts in every backend:
what Renew preserves, when a session counts as expired, whether a request that
read a session just before sign-out can write it back afterwards. Written once
in Store, they cannot differ between backends; written per backend, one of them
would eventually be wrong, and the wrong one would still pass its tests.

# Two timeouts

Idle asks how long a user may walk away and come back. Absolute asks how long a
session may exist at all — which is the only bound on a cookie somebody stole,
because a thief is not idle. Either may be disabled; both may not.

Session.ExpiresAt is the earlier of the two, and is what a cookie's lifetime
should be derived from so that the browser and the store agree on when the
session ended.

# Touching, and what it costs

An idle timeout means every read is also a write, which at any real request rate
is a lot of writes to say the same thing. Policy.Touch is how much of the idle
window must elapse before a read bothers: with a thirty-minute idle timeout and
a one-minute touch interval, one write per minute per active session instead of
one per request.

The precision that buys back is a session whose idle deadline may be up to one
interval stale — so it expires up to one interval *early*, never late. Early is
the safe direction for a security control, which is the only reason the trade is
on offer. Set Touch to zero to refresh on every read.

# The store decides expiry, not the backend

A backend is asked to keep each record until its deadline plus a grace period,
and the store refuses the record the moment the deadline passes. So the backing
store's own expiry is a garbage collector rather than a security control.

That is not incidental. Left to the backend, expiry would be evaluated by a
redis server's clock or a row's timestamp instead of by the clock the policy was
written against and a test can move; a shortened timeout would not apply to
sessions already in flight; and a record already reclaimed cannot be told apart
from one that never existed, so "you idled out" and "no such session" would be
the same answer. The grace period costs retained bytes for expired sessions and
buys all three back. Set it to zero with WithRetentionGrace to give up the
distinction and reclaim at the deadline.

# Renewal is not optional

Renew rotates a session's identifier and carries the payload across. Call it on
every privilege change, sign-in first among them. Without it, an identifier an
attacker planted in a victim's browser before sign-in is still valid after it,
and the attacker is now signed in as the victim — session fixation, which is a
defect in the application rather than in the cookie.

CreatedAt survives renewal, deliberately. If it did not, an application that
correctly renewed on every privilege change would thereby give its sessions an
unbounded life, and the absolute timeout would quietly stop meaning anything.

Renew reports either a new identifier or an error, never both. A caller that
sees an error must assume the old identifier still resolves and refuse the
privilege change that prompted the renewal.

# Identifiers

Minted by NewID from crypto/rand, 256 bits, base64url. Not identifiers.New: an
xid is a timestamp, a machine identifier, a process identifier, and a counter,
which is sortable by design and guessable by construction. That is a feature
everywhere else in this module and a vulnerability here.

Identifiers are bearer credentials, so nothing in this package puts one on a
span or in a log line. What is attached describes a session without naming it.

# Choosing a backend

sessions/cache runs on any cache.Cache — redis for a fleet, memory for tests.
It is the default answer: sessions are short-lived, read on every request, and a
lost session is a sign-in rather than a lost record.

sessions/database survives cache loss, and is the answer when a sign-out has to
be enforceable or a flush must not sign everybody out at once. It also enforces
one thing the cache backend can only approximate: Update is a single UPDATE that
touches nothing if the row is gone, so a request that read a session immediately
before it was signed out cannot write it back afterwards. The cache backend
checks first and then writes, which narrows that window to two adjacent round
trips rather than closing it.

# What T must be

Whatever the chosen backend can round-trip: a concrete struct with exported
fields. The cache backend serializes through its provider's codec (CBOR by
default, gob available), the database backend through an encoding.Codec.

Every record carries a Version. A record written by a different shape reads as
absent rather than being decoded into the current shape, so changing T is a
wave of re-logins rather than users holding somebody else's fields. Bump
recordVersion when Record itself changes shape; sessions_stale_records counts
what that discards.

# Watching it

	sessions_expired          by reason: absolute or idle. A shift toward
	                          absolute usually means the idle timeout is longer
	                          than anyone thinks.
	sessions_touch_failures   idle deadlines that could not be refreshed. The
	                          reads still succeeded; the sessions will expire on
	                          their old schedule.
	sessions_backend_errors   backend health. Absent sessions are not counted
	                          here — they are not errors.
	sessions_stale_records    records discarded for carrying another version;
	                          expected to spike once after a shape change.
	sessions_created          new sessions.
	sessions_renewed          identifier rotations. Should track sign-ins; if it
	                          does not, something is not renewing.
	sessions_ended            explicit sign-outs.
	sessions_touches          idle deadline refreshes.
	sessions_latency_ms       by operation: new, get, save, renew, delete.
*/
package sessions
