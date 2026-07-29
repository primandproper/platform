package memory

import (
	"strconv"
	"testing"
	"time"

	"github.com/shoenig/test/must"
)

type benchItem struct {
	Name string `json:"name"`
}

func BenchmarkInMemoryCache(b *testing.B) {
	c, err := NewInMemoryCache[benchItem](0)
	must.NoError(b, err)

	ctx := b.Context()
	val := &benchItem{Name: "value"}
	must.NoError(b, c.Set(ctx, "key", val))

	b.Run("Get", func(b *testing.B) {
		for b.Loop() {
			_, _ = c.Get(ctx, "key")
		}
	})

	b.Run("Set", func(b *testing.B) {
		for b.Loop() {
			_ = c.Set(ctx, "key", val)
		}
	})
}

// BenchmarkInMemoryCache_Janitor prices the janitor against the write path it
// contends with. The sweep takes the read lock to scan and the write lock to
// delete, so the cost it imposes is on concurrent writers, not on itself — a
// benchmark of the sweep alone would report a number nobody pays.
//
// The interval is deliberately short relative to the benchmark's runtime so
// sweeps actually land during the measured loop.
func BenchmarkInMemoryCache_Janitor(b *testing.B) {
	val := &benchItem{Name: "value"}

	run := func(b *testing.B, opts ...Option[benchItem]) {
		b.Helper()

		c, err := NewInMemoryCache[benchItem](time.Millisecond, opts...)
		must.NoError(b, err)

		ctx := b.Context()
		var i int
		for b.Loop() {
			i++
			_ = c.Set(ctx, strconv.Itoa(i%1024), val)
		}
	}

	b.Run("Off", func(b *testing.B) {
		run(b)
	})

	b.Run("On", func(b *testing.B) {
		run(b, WithJanitor[benchItem](b.Context(), time.Millisecond))
	})
}
