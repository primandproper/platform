package syncmap

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

var errBody = errors.New("body failed")

func TestNew(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := New[string, int]()

		test.EqOp(t, 0, m.Len())

		m.Set("a", 1)

		value, ok := m.Get("a")
		test.True(t, ok)
		test.EqOp(t, 1, value)
	})
}

func TestFrom(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1, "b": 2})

		test.EqOp(t, 2, m.Len())

		value, ok := m.Get("b")
		test.True(t, ok)
		test.EqOp(t, 2, value)
	})

	T.Run("with a nil map", func(t *testing.T) {
		t.Parallel()

		m := From[string, int](nil)

		test.EqOp(t, 0, m.Len())

		m.Set("a", 1)

		test.EqOp(t, 1, m.Len())
	})
}

// The zero value has no backing map until something writes to it, which is the
// one state every other method has to tolerate.
func TestMap_zeroValue(T *testing.T) {
	T.Parallel()

	T.Run("reads before the first write", func(t *testing.T) {
		t.Parallel()

		var m Map[string, int]

		value, ok := m.Get("absent")
		test.False(t, ok)
		test.EqOp(t, 0, value)
		test.False(t, m.Has("absent"))
		test.EqOp(t, 0, m.Len())
		test.SliceEmpty(t, m.Keys())
		test.MapEmpty(t, m.Clone())

		m.Delete("absent")
	})

	T.Run("the first write allocates", func(t *testing.T) {
		t.Parallel()

		var m Map[string, int]

		m.Set("a", 1)

		value, ok := m.Get("a")
		test.True(t, ok)
		test.EqOp(t, 1, value)
	})

	T.Run("WithLock allocates before the body runs", func(t *testing.T) {
		t.Parallel()

		var m Map[string, int]

		must.NoError(t, m.WithLock(func(m map[string]int) error {
			must.NotNil(t, m)
			m["a"] = 1

			return nil
		}))

		test.EqOp(t, 1, m.Len())
	})

	T.Run("WithRLock views an unallocated map", func(t *testing.T) {
		t.Parallel()

		var m Map[string, int]

		must.NoError(t, m.WithRLock(func(r View[string, int]) error {
			test.EqOp(t, 0, r.Len())
			test.False(t, r.Has("a"))

			seen := 0
			for range r.All() {
				seen++
			}
			test.EqOp(t, 0, seen)

			return nil
		}))
	})
}

func TestMap_Set(T *testing.T) {
	T.Parallel()

	T.Run("overwrites an existing key", func(t *testing.T) {
		t.Parallel()

		m := New[string, int]()

		m.Set("a", 1)
		m.Set("a", 2)

		value, ok := m.Get("a")
		test.True(t, ok)
		test.EqOp(t, 2, value)
		test.EqOp(t, 1, m.Len())
	})
}

func TestMap_Delete(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1, "b": 2})

		m.Delete("a")

		test.False(t, m.Has("a"))
		test.True(t, m.Has("b"))
	})

	T.Run("with an absent key", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1})

		m.Delete("nope")

		test.EqOp(t, 1, m.Len())
	})
}

func TestMap_Keys(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1, "b": 2, "c": 3})

		keys := m.Keys()
		slices.Sort(keys)

		test.Eq(t, []string{"a", "b", "c"}, keys)
	})
}

func TestMap_Clone(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1, "b": 2})

		clone := m.Clone()

		test.Eq(t, map[string]int{"a": 1, "b": 2}, clone)
	})

	T.Run("the caller owns the result", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1})

		clone := m.Clone()
		clone["a"] = 99
		clone["b"] = 2

		value, ok := m.Get("a")
		test.True(t, ok)
		test.EqOp(t, 1, value)
		test.False(t, m.Has("b"))
	})

	T.Run("is never nil", func(t *testing.T) {
		t.Parallel()

		var m Map[string, int]

		clone := m.Clone()

		must.NotNil(t, clone)
		test.MapEmpty(t, clone)
	})
}

func TestMap_WithLock(T *testing.T) {
	T.Parallel()

	T.Run("makes several accesses one critical section", func(t *testing.T) {
		t.Parallel()

		m := New[string, int]()

		var resolved int

		must.NoError(t, m.WithLock(func(m map[string]int) error {
			if existing, ok := m["a"]; ok {
				resolved = existing

				return nil
			}

			m["a"] = 42
			resolved = 42

			return nil
		}))

		test.EqOp(t, 42, resolved)
		test.EqOp(t, 1, m.Len())
	})

	T.Run("returns the body's error unwrapped", func(t *testing.T) {
		t.Parallel()

		m := New[string, int]()

		err := m.WithLock(func(map[string]int) error {
			return errBody
		})

		must.ErrorIs(t, err, errBody)
		test.EqOp(t, errBody, err)
	})

	// Mutual exclusion, not atomicity of effect: the doc promises this and the
	// test is what keeps a well-meaning rollback from being added later.
	T.Run("does not roll back the body's writes on error", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1})

		err := m.WithLock(func(m map[string]int) error {
			m["b"] = 2
			delete(m, "a")

			return errBody
		})

		must.ErrorIs(t, err, errBody)
		test.False(t, m.Has("a"))
		test.True(t, m.Has("b"))
	})

	T.Run("releases the lock on panic and re-raises", func(t *testing.T) {
		t.Parallel()

		m := New[string, int]()

		func() {
			defer func() {
				test.Eq(t, any("boom"), recover())
			}()

			_ = m.WithLock(func(m map[string]int) error {
				m["written before the panic"] = 1

				panic("boom")
			})
		}()

		// Still lockable, in both modes, without hanging.
		m.Set("a", 1)
		must.NoError(t, m.WithLock(func(m map[string]int) error {
			test.EqOp(t, 2, len(m))

			return nil
		}))
		must.NoError(t, m.WithRLock(func(r View[string, int]) error {
			test.EqOp(t, 2, r.Len())

			return nil
		}))
	})
}

func TestMap_WithRLock(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1, "b": 2})

		var total int

		must.NoError(t, m.WithRLock(func(r View[string, int]) error {
			test.EqOp(t, 2, r.Len())

			for _, value := range r.All() {
				total += value
			}

			return nil
		}))

		test.EqOp(t, 3, total)
	})

	T.Run("returns the body's error unwrapped", func(t *testing.T) {
		t.Parallel()

		m := New[string, int]()

		err := m.WithRLock(func(View[string, int]) error {
			return errBody
		})

		test.EqOp(t, errBody, err)
	})

	T.Run("concurrent readers proceed together", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1})

		const readers = 4

		var (
			wg      sync.WaitGroup
			entered = make(chan struct{}, readers)
			release = make(chan struct{})
		)

		wg.Add(readers)
		for range readers {
			go func() {
				defer wg.Done()

				_ = m.WithRLock(func(r View[string, int]) error {
					entered <- struct{}{}
					// Every reader has to be inside before any of them leaves,
					// which a mutual-exclusion implementation could not satisfy.
					<-release

					return nil
				})
			}()
		}

		for range readers {
			<-entered
		}
		close(release)

		wg.Wait()
	})

	T.Run("releases the lock on panic and re-raises", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1})

		func() {
			defer func() {
				test.Eq(t, any("boom"), recover())
			}()

			_ = m.WithRLock(func(View[string, int]) error {
				panic("boom")
			})
		}()

		m.Set("b", 2)
		test.EqOp(t, 2, m.Len())
	})
}

func TestView(T *testing.T) {
	T.Parallel()

	T.Run("Get reports presence", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1})

		must.NoError(t, m.WithRLock(func(r View[string, int]) error {
			value, ok := r.Get("a")
			test.True(t, ok)
			test.EqOp(t, 1, value)

			missing, ok := r.Get("b")
			test.False(t, ok)
			test.EqOp(t, 0, missing)

			return nil
		}))
	})

	T.Run("All stops early when the body breaks", func(t *testing.T) {
		t.Parallel()

		m := From(map[string]int{"a": 1, "b": 2, "c": 3})

		var seen int

		must.NoError(t, m.WithRLock(func(r View[string, int]) error {
			for range r.All() {
				seen++

				break
			}

			return nil
		}))

		test.EqOp(t, 1, seen)
	})
}

// The race detector is the assertion here: the point operations and both lock
// scopes are mixed against the same Map from several goroutines at once.
func TestMap_concurrentAccess(t *testing.T) {
	t.Parallel()

	const (
		goroutines = 8
		iterations = 200
		keys       = 16
	)

	m := New[int, int]()

	var wg sync.WaitGroup

	wg.Add(goroutines)
	for g := range goroutines {
		go func() {
			defer wg.Done()

			for i := range iterations {
				key := (g + i) % keys

				switch i % 8 {
				case 0:
					m.Set(key, i)
				case 1:
					_, _ = m.Get(key)
				case 2:
					m.Delete(key)
				case 3:
					_ = m.Has(key)
				case 4:
					_ = m.Len()
					_ = m.Keys()
					_ = m.Clone()
				case 5:
					// Get-or-create, the shape the type exists for.
					_ = m.WithLock(func(m map[int]int) error {
						if _, ok := m[key]; !ok {
							m[key] = i
						}

						return nil
					})
				case 6:
					_ = m.WithRLock(func(r View[int, int]) error {
						total := 0
						for _, value := range r.All() {
							total += value
						}
						_ = total

						return nil
					})
				case 7:
					_ = m.WithLock(func(m map[int]int) error {
						delete(m, key)
						m[key+keys] = i

						return errBody
					})
				}
			}
		}()
	}

	wg.Wait()

	// Every key ever written is in range, and nothing outside it survived.
	for _, key := range m.Keys() {
		test.True(t, key >= 0 && key < 2*keys)
	}
}
