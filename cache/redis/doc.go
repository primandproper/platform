/*
Package redis is a cache.Cache backed by Redis, in either single-node or cluster
mode.

# What choosing it commits you to

The store is shared and outlives the process, which is the reason to pick it:
every replica sees the same entries, an invalidation performed anywhere is
effective everywhere, and a deploy does not start cold.

The price is that every operation is a network call, so this provider has
failure modes the memory one does not. Calls go through a circuit breaker, and a
tripped one reports cache.ErrUnavailable — deliberately not cache.ErrNotFound,
because a caller that needs to distinguish "absent" from "could not ask" must not
be told the former when the latter is true. Batched reads and writes exist on the
interface because round trips are the cost that matters here: GetMany, SetMany,
and DeleteMany do in one call what a loop would do in n.

Values are encoded on write and decoded into a fresh value on read, so nothing is
shared between the caller and the store the way it is in the memory provider.

# Namespaces are what make whole-cache operations possible

A Redis database may hold more than this cache's entries. With cfg.Namespace set,
every key is transparently prefixed with it — callers still use bare keys — and
the prefix is what lets Flush and an empty-prefix DeleteByPrefix delete exactly
what this cache owns. Without one they report cache.ErrNamespaceRequired rather
than guess.

Entries carry no record of the codec that wrote them, so pointing a cache with a
new codec at keys warmed by the old one produces decode errors until they expire.
Give the new codec its own namespace when switching.

# Cluster mode

A cluster is inferred from more than one address and can also be declared
outright with cfg.Cluster, which is necessary when the cluster is reached through
a single seed — a multi-key command against a cluster misread as single-node
fails with CROSSSLOT. In cluster mode, prefix deletion fans out across masters,
since SCAN answers only for the node it was sent to.
*/
package redis
