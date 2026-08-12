package syncmap_test

import (
	"fmt"
	"slices"

	"github.com/primandproper/platform-go/v10/syncmap"
)

func ExampleMap() {
	var counts syncmap.Map[string, int]

	counts.Set("a", 1)
	counts.Set("b", 2)

	value, ok := counts.Get("a")
	fmt.Println(value, ok, counts.Len())

	counts.Delete("a")
	fmt.Println(counts.Has("a"))

	// Output:
	// 1 true 2
	// false
}

// The get-or-create shape: the read, the miss, the construction and the insert
// are one critical section, so two callers cannot each build a connection.
//
// Note the shadowing — the parameter is named for the map, so inside the body
// there is no receiver whose methods would deadlock on the lock already held.
func ExampleMap_WithLock() {
	type connection struct{ host string }

	var (
		conns syncmap.Map[string, *connection]
		dials int
	)

	connFor := func(host string) (*connection, error) {
		var resolved *connection

		if err := conns.WithLock(func(conns map[string]*connection) error {
			if c, ok := conns[host]; ok {
				resolved = c

				return nil
			}

			dials++
			c := &connection{host: host}
			conns[host] = c
			resolved = c

			return nil
		}); err != nil {
			return nil, err
		}

		return resolved, nil
	}

	for range 3 {
		if _, err := connFor("db.example"); err != nil {
			fmt.Println(err)
		}
	}

	fmt.Println(dials, conns.Len())

	// Output: 1 1
}

// The read-snapshot shape: a View exposes no way to write, which is what makes
// handing the caller a read lock safe.
func ExampleMap_WithRLock() {
	subscribers := syncmap.From(map[string]int{"alice": 2, "bob": 1, "carol": 3})

	var (
		names []string
		total int
	)

	if err := subscribers.WithRLock(func(r syncmap.View[string, int]) error {
		for name, streams := range r.All() {
			names = append(names, name)
			total += streams
		}

		return nil
	}); err != nil {
		fmt.Println(err)
	}

	slices.Sort(names)
	fmt.Println(names, total)

	// Output: [alice bob carol] 6
}

// Work that blocks belongs outside the lock. Clone under it, release it, then
// take as long as the work takes without every other caller waiting.
func ExampleMap_Clone() {
	streams := syncmap.From(map[string]int{"alice": 1, "bob": 2})

	snapshot := streams.Clone()

	// The lock is not held here, so send may block for as long as it likes.
	sent := make([]string, 0, len(snapshot))
	for name := range snapshot {
		sent = append(sent, name)
	}

	slices.Sort(sent)
	fmt.Println(sent)

	// Output: [alice bob]
}
