package noop

import (
	"context"

	textsearch "github.com/primandproper/platform-go/v9/search/text"
)

var _ textsearch.Index[any] = (*indexManager[any])(nil)

// indexManager is a noop Index.
type indexManager[T any] struct{}

// NewIndexManager returns a no-op Index.
func NewIndexManager[T any]() textsearch.Index[T] {
	return &indexManager[T]{}
}

// Search is a no-op method. It returns no hits and no next cursor, so a caller
// paging through results terminates on the first call rather than looping.
func (*indexManager[T]) Search(context.Context, textsearch.SearchRequest) (*textsearch.SearchResults[T], error) {
	return &textsearch.SearchResults[T]{Hits: []*T{}}, nil
}

// Index is a no-op method.
func (*indexManager[T]) Index(context.Context, string, any) error {
	return nil
}

// Delete is a no-op method.
func (*indexManager[T]) Delete(context.Context, string) error {
	return nil
}

// Wipe is a no-op method.
func (*indexManager[T]) Wipe(context.Context) error {
	return nil
}
