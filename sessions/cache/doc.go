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

Where renewal has to be atomic, or where flushing the cache must not sign
everybody out, use sessions/database.

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
