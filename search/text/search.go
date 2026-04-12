package textsearch

import (
	"context"
)

type ( // IndexSearcher is our wrapper interface for querying a text search index.
	IndexSearcher[T any] interface {
		Search(ctx context.Context, query string) (ids []*T, err error)
	}

	// IndexManager is our wrapper interface for a text search index.
	IndexManager interface {
		Index(ctx context.Context, id string, value any) error
		Delete(ctx context.Context, id string) (err error)
		Wipe(ctx context.Context) error
	}

	// Index is our wrapper interface for a text search index.
	Index[T any] interface {
		IndexSearcher[T]
		IndexManager
	}
)
