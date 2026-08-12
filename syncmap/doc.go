/*
Package syncmap provides a map guarded by an RWMutex, where a compound
operation is a lock scope rather than a convention.

A mutex beside a map is three lines, and the three lines are not the problem.
The problem is the critical section that spans more than one access — read,
miss, construct, insert — where the correctness lives entirely in a pairing
that the type system is not holding and that a reader has to verify by eye,
per site, forever. [Map.WithLock] makes that pairing the type's problem:

	func (r *Relay) publisherFor(ctx context.Context, topic string) (messagequeue.Publisher, error) {
		var publisher messagequeue.Publisher

		if err := r.publishers.WithLock(func(publishers map[string]messagequeue.Publisher) error {
			if p, ok := publishers[topic]; ok {
				publisher = p

				return nil
			}

			p, err := r.provider.NewPublisher(ctx, topic)
			if err != nil {
				return err
			}

			publishers[topic] = p
			publisher = p

			return nil
		}); err != nil {
			return nil, err
		}

		return publisher, nil
	}

There is no publishersMu field to keep beside the map, no pairing to check, and
the lock is released on the error path and on a panic because the type did it.
This is what database.RunInTransaction does for a transaction, for a mutex: the
caller does not acquire, does not release, and cannot forget either.

# The write path hands out the naked map; the read path does not

[Map.WithLock] passes the callback a map[K]V, because m[k] = v, delete(m, k),
len(m) and range are the operations the caller came for, and because the whole
advantage over sync.Map is that a critical section can hold arbitrary map code
rather than a fixed method set.

[Map.WithRLock] passes a [View], because a naked map handed out under a read
lock is writable, and writing to it is a data race with no compiler or vet
diagnostic anywhere. The asymmetry is the point: the naked map appears exactly
where mutation is licensed. A View costs the caller v.All() instead of range.

# Hazards

Four, and the type addresses two of them.

Escape. Neither the map nor a View stops being usable when the callback
returns. A body that stashes either in a struct field or hands it to a goroutine
has moved the access outside the lock, and nothing will say so. The map handed
to a WithLock body, and any value read out of it that aliases shared state, is
for the body.

Reentrancy. sync.RWMutex is not reentrant. Calling m.Get from inside
m.WithLock — or m.Set from inside m.WithRLock — deadlocks with no diagnostic
and no timeout. Shadow the receiver with the parameter, as the example above
does, and the mistake is hard to type: inside the body, the guarded map is what
the name refers to.

Blocking work in the body. WithLock makes it easier to hold a lock across a
network call than the hand-rolled version did, because the body is arbitrary
code — the one above builds a publisher under the lock deliberately, so that
two callers cannot build two. If the body does I/O that every other caller
should not be waiting behind, you wanted [Map.Clone]: snapshot under the lock,
release it, then do the slow work.

No rollback. WithLock is mutual exclusion, not atomicity of effect. It returns
fn's error unwrapped, and what fn wrote before failing stays written. There is
no WithTransaction that clones on entry and installs on success, because a
shallow copy still shares every pointer a V holds, and a rollback guarantee
that is a half-truth is worse than an absent one.

# What is deliberately absent

No context.Context. Mutex acquisition is uninterruptible, and a ctx that cannot
be honored is a lie. distributedlock.ScopedLocker.WithLock does take one, on
purpose: the arity difference is the honest signal that one of these can block
on a network and the other cannot.

No observability. This is a data structure. A component that wants spans around
its map access keeps its observer and holds a Map inside it; the type replaces
the mechanics, not the wrapper.

No GetOrSet, LoadOrStore, or Update. Each is a body WithLock already writes,
and growing one method per compound operation is how sync.Map came to be a poor
fit for this shape in the first place.

# Why not sync.Map

It has no Len, its values are any, it offers no way to make two accesses one
critical section, and it is tuned for the disjoint-key, append-mostly case —
which describes approximately none of the maps in this module.
*/
package syncmap
