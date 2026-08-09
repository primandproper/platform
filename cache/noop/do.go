package noop

import (
	"github.com/primandproper/platform-go/v10/cache"

	"github.com/samber/do/v2"
)

// RegisterCache registers this implementation under two keys: its own type,
// *Cache[T], and cache.Cache[T]. Both resolve to the same cache.
//
// Naming the noop is the whole point of registering it. A container that simply
// leaves the cache out gets a do.Invoke failure, which is the right answer for a
// dependency somebody forgot; a container that registers this one has said it
// wants reads that always miss and writes that go nowhere.
//
// It is generic because a cache holds values of one concrete type; each cached
// type is registered separately. Nothing is invoked and nothing is configurable:
// a cache that stores nothing has no knobs and no observability to attach.
func RegisterCache[T any](i do.Injector) {
	do.Provide(i, func(do.Injector) (*Cache[T], error) {
		return NewCache[T](), nil
	})

	// Cannot fail: *Cache[T] implements cache.Cache[T] — the compiler says so
	// at the top of noop.go — and the service it aliases was just provided.
	do.MustAs[*Cache[T], cache.Cache[T]](i)
}
