/*
Package memory is a cache.Cache held in a map in this process.

# What choosing it commits you to

Nothing here has a network, and that is most of what distinguishes it from the
redis provider. There is no circuit breaker, no cache.ErrUnavailable, no
connection to lose and no partial batch: a read either finds the key or reports
cache.ErrNotFound, Ping always succeeds, and Flush needs no namespace because
this cache wholly owns its store. Code written against *Cache[T] rather than
against cache.Cache[T] therefore carries none of the handling those
possibilities force on it.

What it gives up is everything the shared store was for. Entries live in one
process, so two replicas have two caches that agree about nothing, a restart is
a cold cache, and a write on one instance is invisible to the others — including
a delete, which is the case that bites: an invalidation that reaches only the
replica that performed it leaves the stale value being served everywhere else
until it expires. A cache whose entries must be consistent across replicas, or
must survive a deploy, wants the redis provider.

# Values are shared, not copied

Set stores the pointer it is given and Get hands that same pointer back. Nothing
is serialized, so a caller that mutates a value it read from the cache mutates
what every other reader sees, with no lock held. Store values that are treated
as immutable, or copy on the way out. The redis provider does not have this
property — it encodes on write and decodes into a fresh value on read — so code
that relies on either behavior is not portable between the two.

# Bounds

By default the map is bounded only by expiry, and expired entries are reclaimed
lazily, on the read that finds them or when the key is overwritten. A key
written once and never read again therefore holds its memory indefinitely: pass
WithJanitor to sweep on a timer, WithMaxEntries to bound the map by count, or
both. A size bound requires an explicit eviction policy, since the policy
decides what a full cache forgets.

WithLoader turns the cache read-through, computing what a read misses and
collapsing concurrent misses on one key into a single computation.
*/
package memory
