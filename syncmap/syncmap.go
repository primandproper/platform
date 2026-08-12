package syncmap

import (
	"iter"
	"maps"
	"slices"
	"sync"
)

// Map is a map guarded by an [sync.RWMutex]. The zero value is an empty, ready
// Map: the backing map is allocated by the first write, under the write lock.
//
// A Map contains a mutex, so it is used through a pointer and never copied —
// go vet's copylocks reports a copy as an error. A Map is fine as a value field
// of a struct that is itself used through a pointer, which is how a hand-rolled
// mutex-and-map pair was already being held.
//
// The point operations take and release the lock themselves. Two of them in a
// row are two critical sections, and anything that has to be one — read, miss,
// build, insert — belongs in [Map.WithLock] or [Map.WithRLock].
type Map[K comparable, V any] struct {
	m  map[K]V
	mu sync.RWMutex
}

// New returns an empty Map with its backing map already allocated. The zero
// value works too; this exists for the call sites that want an initializer.
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{m: make(map[K]V)}
}

// From returns a Map guarding m, taking ownership of it. The caller must not
// retain m: writes through the original reference are exactly the unguarded
// accesses this type exists to prevent. Pass maps.Clone(m) to keep a copy.
//
// A nil m is accepted and behaves as the zero value.
func From[K comparable, V any](m map[K]V) *Map[K, V] {
	return &Map[K, V]{m: m}
}

// Get returns the value stored under key, and whether it was present.
func (m *Map[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	value, ok := m.m[key]

	return value, ok
}

// Set stores value under key.
func (m *Map[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensure()

	m.m[key] = value
}

// Delete removes key. Deleting an absent key is not an error.
func (m *Map[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.m, key)
}

// Has reports whether key is present.
func (m *Map[K, V]) Has(key K) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.m[key]

	return ok
}

// Len returns the number of entries.
func (m *Map[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.m)
}

// Keys returns an unordered snapshot of the keys. It is a snapshot: a key may
// have been deleted, and another added, by the time the caller reads it.
func (m *Map[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return slices.Collect(maps.Keys(m.m))
}

// Clone returns a shallow copy of the map, which the caller owns and may write
// to freely. It is never nil, and it is shallow: a V holding a pointer is
// shared with the entry still in the Map.
//
// Clone is the answer to "I need to do slow work with these entries". Copy
// under the lock, release it, then do the work — rather than holding the lock
// across the work inside a WithLock body.
func (m *Map[K, V]) Clone() map[K]V {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.m == nil {
		return map[K]V{}
	}

	return maps.Clone(m.m)
}

// WithLock calls fn with the backing map, under the write lock, and returns
// fn's error unchanged. It is the reason this type exists: every access fn
// makes is one critical section, so a read, a miss, a construction and an
// insert cannot interleave with another caller doing the same.
//
// The parameter is the naked map, so the body is ordinary map code:
//
//	if err := publishers.WithLock(func(publishers map[string]Publisher) error {
//		if p, ok := publishers[topic]; ok {
//			resolved = p
//
//			return nil
//		}
//		// ... build it, insert it
//		return nil
//	}); err != nil {
//		return nil, err
//	}
//
// Shadowing the receiver's name with the parameter, as above, is the
// convention: inside the body the guarded map is the map, and the name of the
// Map — whose methods all deadlock in there — is not in scope.
//
// WithLock is mutual exclusion, not atomicity of effect. When fn returns an
// error, whatever fn already wrote to the map stays written; nothing is rolled
// back. A body that must not leave a partial write has to arrange that itself,
// by computing what it will write before it writes any of it.
//
// The lock is released whether fn returns, returns an error, or panics; a panic
// propagates to the caller with the Map still usable.
func (m *Map[K, V]) WithLock(fn func(m map[K]V) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ensure()

	return fn(m.m)
}

// WithRLock calls fn with a read-only [View] of the map, under the read lock,
// and returns fn's error unchanged. Concurrent WithRLock calls proceed together.
//
// It hands out a View rather than the map because a map handed out under a read
// lock is still writable, and writing to it is a data race that neither the
// compiler nor go vet will mention. The naked map is [Map.WithLock]'s parameter
// precisely because that is where mutation is licensed.
//
// The lock is released whether fn returns, returns an error, or panics.
func (m *Map[K, V]) WithRLock(fn func(r View[K, V]) error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return fn(View[K, V]{m: m.m})
}

// ensure allocates the backing map, so that the zero value is writable. The
// caller holds the write lock.
func (m *Map[K, V]) ensure() {
	if m.m == nil {
		m.m = make(map[K]V)
	}
}

// View is a read-only window onto a [Map], valid for the duration of the
// [Map.WithRLock] call that produced it. A View retained past that call reads a
// map that may be concurrently written, which is a data race — View removes the
// likelier mistake, writing through a read lock, and not that one.
type View[K comparable, V any] struct {
	m map[K]V
}

// Get returns the value stored under key, and whether it was present.
func (v View[K, V]) Get(key K) (V, bool) {
	value, ok := v.m[key]

	return value, ok
}

// Has reports whether key is present.
func (v View[K, V]) Has(key K) bool {
	_, ok := v.m[key]

	return ok
}

// Len returns the number of entries.
func (v View[K, V]) Len() int {
	return len(v.m)
}

// All iterates the entries in an unspecified order. Like the View itself, the
// sequence is only valid for the duration of the WithRLock call — collect what
// is needed and range over that if it has to outlive the body.
func (v View[K, V]) All() iter.Seq2[K, V] {
	return maps.All(v.m)
}
