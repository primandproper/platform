/*
Package cache stores session records in a cache.Cache.

It is the default backend. Sessions are short-lived, read on every request, and
a lost one costs a sign-in rather than a record — which is exactly the shape a
cache is good at. Pointed at redis it serves a fleet; pointed at cache/memory it
serves a test.

	c, _ := cachecfg.NewCache[sessions.Record[Principal]](ctx, &cfg.Cache)
	backend, _ := sessionscache.NewBackend(c)
	store, _ := sessions.NewStore(backend, sessions.WithIdleTimeout(30*time.Minute))

The cache's configured default expiry is never consulted: every write carries
the deadline the Store computed from its Policy, so the two cannot disagree
about when a session ends.

# What it cannot do

Update is safe against a concurrent sign-out: it goes through
cache.SetIfPresent, so the write and the check that the record still exists are
one operation, and a request that loaded a session just before sign-out cannot
write it back afterwards.

Rename is not. It writes the new identifier and deletes the old one, and no
conditional write spans two keys — that needs a transaction, which is what
sessions/database has and this does not. A sign-out landing mid-renewal can
therefore still leave the renewed session alive.

Neither is the enumeration. ListHeld, DeleteHeld and DeleteAllHeld all report
sessions.ErrNoPrincipalIndex: a key-value store answers what is under a key, and
"which keys belong to this person" needs a second structure maintained beside
the records — which is a second account of which sessions are live, and the
moment it disagrees with the first, a revocation has removed a session the list
still shows or spared one it does not. So this backend does not keep one and
says so, rather than answering an empty list that reads as "you are signed in
nowhere else" to somebody who is.

The attribution itself does round-trip: a session established through
sessions.Store.NewFor comes back from Get carrying its holder and its metadata,
because those travel in the record. It is only the read that starts from a
person rather than from an identifier that a cache cannot serve.

Where renewal has to be atomic, where flushing the cache must not sign everybody
out, or where a user has to be able to see and end their own sessions, use
sessions/database.

# Choosing the cache

The memory provider is per-process. Two replicas do not share sessions, so a
user is signed in to whichever replica their request lands on and signed out of
the others — fine for tests and single-process services, wrong for anything
behind a load balancer.

Redis wants a namespace of its own. Session records share a keyspace with
whatever else is in that cache otherwise, and a Flush meant for something else
signs every user out.
*/
package cache
