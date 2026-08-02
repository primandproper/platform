package noop

import (
	"context"

	"github.com/primandproper/platform-go/v9/cache"
)

var _ cache.Cache[any] = (*Cache[any])(nil)

// Cache is a no-op Cache.
type Cache[T any] struct{}

// NewCache returns a no-op Cache.
func NewCache[T any]() cache.Cache[T] {
	return &Cache[T]{}
}

// Get always returns ErrNotFound.
func (*Cache[T]) Get(context.Context, string) (*T, error) {
	return nil, cache.ErrNotFound
}

// Set is a no-op.
func (*Cache[T]) Set(context.Context, string, *T, ...cache.WriteOption) error {
	return nil
}

// Delete is a no-op.
func (*Cache[T]) Delete(context.Context, string) error {
	return nil
}

// GetMany always returns an empty map.
func (*Cache[T]) GetMany(context.Context, []string) (map[string]*T, error) {
	return map[string]*T{}, nil
}

// SetMany is a no-op.
func (*Cache[T]) SetMany(context.Context, map[string]*T, ...cache.WriteOption) error {
	return nil
}

// DeleteMany is a no-op.
func (*Cache[T]) DeleteMany(context.Context, []string) error {
	return nil
}

// DeleteByPrefix is a no-op.
func (*Cache[T]) DeleteByPrefix(context.Context, string) error {
	return nil
}

// Flush is a no-op.
func (*Cache[T]) Flush(context.Context) error {
	return nil
}

// Ping is a no-op.
func (*Cache[T]) Ping(context.Context) error {
	return nil
}

// Close satisfies the interface and releases nothing.
func (c *Cache[T]) Close() error {
	return nil
}
