/*
Package cache stores action link records in a cache.Cache.

It is the store links started with, and the one a deployment already running
Redis wants. A link is a small record read once, written once more, and then
forgotten a day later — the shape a cache is good at — and Redis is shared by
every process that has to see it.

	c, _ := cachecfg.NewCache[links.Record](ctx, &cfg.Cache)
	locker, _ := distributedlockcfg.NewScopedLocker(ctx, &cfg.Lock, nil)
	store, _ := linkscache.New(c, locker)
	minter, _ := links.NewMinter(store, links.WithAction(...))

The cache's configured default expiry is never consulted: every write carries
the window the Minter computed from the record's own timestamps, so the two
cannot disagree about when a record stops answering.

# The locker is not optional, and this is why

Single use means a read and a write that nothing can interleave. A cache offers
no way to express that across two keys' worth of round trips, so this store
takes a distributedlock.ScopedLocker and holds it across both halves of a
resolution. Without one, two requests carrying the same token both read
"active" and both proceed — which is the entire failure links exists to
prevent, arriving silently and only under the concurrency an attacker supplies
deliberately. The noop locker acquires unconditionally, so every sequential
test still passes.

links/database needs no locker at all: a guarded UPDATE inside one transaction
is the same promise, decided by the server.

# What this store cannot do

It cannot revoke by subject, and it does not pretend to. A Minter built over it
reports links.ErrSubjectRevocationUnsupported from RevokeForSubject rather than
approximating an answer.

The obstacle is the seam rather than the effort. A cache.Cache reads by key, and
the key here has to be the digest of the token, because redemption arrives
holding the token and nothing else — so the subject cannot be folded into the
key and there is no read that finds records by it. Serving the write anyway
would mean keeping a subject-to-IDs set beside the records: a second write on
every mint that can fail independently of the first, an amendment on every
resolution, a set that drifts from the records it points at whenever either
write loses, and no type in a cache.Cache[links.Record] to hold it in without
abusing Record. It is, precisely, the "second, weaker copy of a log the
application already keeps" that links declined to build — and building it here
would not make it a better idea, only a less visible one.

So the answer for a cache-backed deployment is the one links documented for
every deployment before the table existed: query the audit log for mints of that
action for that subject with no corresponding redemption, and call
links.Minter.Revoke per result. A deployment that would rather have the
statement wants links/database, which has subject as a column and answers it in
one UPDATE.

# Choosing the cache

The memory provider is per-process, and for links that is a stronger warning
than it is elsewhere. A link is minted by whatever builds the email — often an
async handler consuming a data-change topic — and redeemed by whatever serves
the click, which is the API server. Those are two deployments, so a per-process
cache does not make a link less durable, it makes it unredeemable: the record
was written where the redeemer never reads. That holds at one replica each.

Collapsed into a single process it works, at a price: every outstanding link
dies at the next restart or rolling deploy. For a verification link meant to be
clicked hours later, that is most of them. It also needs cache/memory's
WithJanitor to reclaim anything, since an entry written once and never read
again is never lazily evicted.

Redis wants a namespace of its own — see WithKeyPrefix. Link records share a
keyspace with whatever else is in that cache otherwise, and a Flush meant for
something else invalidates every outstanding password reset.

A deployment with no Redis at all wants links/database, which keeps the records
in a table beside the application's own rows and needs neither a cache nor a
lock service.
*/
package cache
