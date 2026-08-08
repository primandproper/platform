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

cache.Cache has no conditional write, so Update reads before it writes rather
than writing only if the record is still there. That check is what stops a
request which loaded a session just before sign-out from writing it back
afterwards — but between the read and the write the record can still be removed,
so the window is narrowed to two adjacent round trips instead of closed. Rename
has the same shape: the new identifier is written and the old one deleted, with
no transaction spanning the two.

Where a sign-out has to be enforceable rather than very-nearly enforceable, or
where flushing the cache must not sign everybody out, use sessions/database.

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
