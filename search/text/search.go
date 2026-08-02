package textsearch

import (
	"context"
)

// DefaultSearchLimit is the page size used when a SearchRequest does not name
// one. It is stated here rather than left to each backend because the backends
// disagree: Elasticsearch defaults to 10 hits and Algolia to 20, so a caller
// that omitted the limit got a different page depending on which one it was
// talking to.
const DefaultSearchLimit = 25

type ( // IndexSearcher is our wrapper interface for querying a text search index.
	IndexSearcher[T any] interface {
		Search(ctx context.Context, req SearchRequest) (*SearchResults[T], error)
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

// SearchRequest is one page of one query.
//
// It is a struct rather than positional parameters because the pagination
// fields are the kind that get added to over time — a filter, a sort, a
// scoring hint — and each addition would otherwise break every implementation.
type SearchRequest struct {
	// Query is the text to search for. An empty query is an error rather than a
	// match-all: every backend here treats it differently, and the one thing
	// none of them means by it is "return the entire index".
	Query string

	// Cursor resumes a previous search. The zero value starts at the beginning.
	// A cursor is only meaningful to the backend that issued it.
	Cursor Cursor

	// Limit is the maximum number of hits to return. Zero means
	// DefaultSearchLimit; backends may cap it lower.
	Limit int
}

// SearchResults is one page of hits, plus the cursor for the next one.
type SearchResults[T any] struct {
	// NextCursor resumes after the last hit in Hits. It is empty when the
	// result set is exhausted, which is how a caller knows to stop — not a
	// short page, since a backend may return fewer hits than requested and
	// still have more.
	NextCursor Cursor

	// Hits are the documents matched, in the backend's relevance order.
	Hits []*T
}

// Done reports whether this is the last page.
func (r *SearchResults[T]) Done() bool {
	return r == nil || r.NextCursor.IsZero()
}
