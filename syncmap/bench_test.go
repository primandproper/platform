package syncmap

import (
	"sync"
	"testing"
)

// guarded is the hand-rolled pair this package replaces, benchmarked as the
// baseline: the question is what the closure and the extra call cost, not
// whether a mutex is faster than a mutex.
type guarded struct {
	m  map[string]int
	mu sync.RWMutex
}

func (g *guarded) get(key string) (int, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	value, ok := g.m[key]

	return value, ok
}

func (g *guarded) set(key string, value int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.m[key] = value
}

func (g *guarded) getOrCreate(key string, build func() int) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	if value, ok := g.m[key]; ok {
		return value
	}

	value := build()
	g.m[key] = value

	return value
}

const benchKey = "key"

func BenchmarkPointRead(b *testing.B) {
	b.Run("syncmap", func(b *testing.B) {
		m := From(map[string]int{benchKey: 1})

		for b.Loop() {
			intSink, boolSink = m.Get(benchKey)
		}
	})

	b.Run("mutex and map", func(b *testing.B) {
		g := &guarded{m: map[string]int{benchKey: 1}}

		for b.Loop() {
			intSink, boolSink = g.get(benchKey)
		}
	})

	b.Run("sync.Map", func(b *testing.B) {
		var s sync.Map
		s.Store(benchKey, 1)

		for b.Loop() {
			value, ok := s.Load(benchKey)
			intSink, boolSink = value.(int), ok
		}
	})
}

func BenchmarkPointWrite(b *testing.B) {
	b.Run("syncmap", func(b *testing.B) {
		m := New[string, int]()

		for b.Loop() {
			m.Set(benchKey, 1)
		}
	})

	b.Run("mutex and map", func(b *testing.B) {
		g := &guarded{m: map[string]int{}}

		for b.Loop() {
			g.set(benchKey, 1)
		}
	})

	b.Run("sync.Map", func(b *testing.B) {
		var s sync.Map

		for b.Loop() {
			s.Store(benchKey, 1)
		}
	})
}

// The compound case, on the hit path — the one the type exists for. sync.Map
// is represented by LoadOrStore, which is as close as it comes and is not the
// same thing: the value has to be built before the call, whether or not it is
// needed.
func BenchmarkGetOrCreate(b *testing.B) {
	build := func() int { return 1 }

	b.Run("syncmap", func(b *testing.B) {
		m := New[string, int]()

		for b.Loop() {
			_ = m.WithLock(func(m map[string]int) error {
				value, ok := m[benchKey]
				if !ok {
					value = build()
					m[benchKey] = value
				}
				intSink = value

				return nil
			})
		}
	})

	b.Run("mutex and map", func(b *testing.B) {
		g := &guarded{m: map[string]int{}}

		for b.Loop() {
			intSink = g.getOrCreate(benchKey, build)
		}
	})

	b.Run("sync.Map", func(b *testing.B) {
		var s sync.Map

		for b.Loop() {
			value, _ := s.LoadOrStore(benchKey, build())
			intSink = value.(int)
		}
	})
}

// Under contention the read lock is the whole point, and it is where sync.Map
// is expected to win a disjoint-key workload it was tuned for.
func BenchmarkParallelRead(b *testing.B) {
	b.Run("syncmap", func(b *testing.B) {
		m := From(map[string]int{benchKey: 1})

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				intSink, boolSink = m.Get(benchKey)
			}
		})
	})

	b.Run("mutex and map", func(b *testing.B) {
		g := &guarded{m: map[string]int{benchKey: 1}}

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				intSink, boolSink = g.get(benchKey)
			}
		})
	})

	b.Run("sync.Map", func(b *testing.B) {
		var s sync.Map
		s.Store(benchKey, 1)

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				value, ok := s.Load(benchKey)
				intSink, boolSink = value.(int), ok
			}
		})
	})
}

var (
	intSink  int
	boolSink bool
)
