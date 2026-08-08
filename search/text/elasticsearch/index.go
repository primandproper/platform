package elasticsearch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/keys"
	textsearch "github.com/primandproper/platform-go/v10/search/text"

	"github.com/elastic/go-elasticsearch/v8/esapi"
)

const (
	// backendName tags cursors this package issues, so one cannot be resumed
	// against a different backend.
	backendName = "elasticsearch"

	// maxSearchLimit caps a single page. Elasticsearch will serve more, but a
	// page this size is already past the point where a caller wants one.
	maxSearchLimit = 200

	// maxResultWindow mirrors Elasticsearch's index.max_result_window default.
	// from + size beyond it is rejected by the server, so pagination stops here
	// with an error a caller can recognize rather than a 500 from the cluster.
	maxResultWindow = 10000
)

var (
	// ErrEmptyQueryProvided indicates an empty query was provided as input.
	ErrEmptyQueryProvided = platformerrors.New("empty search query provided")

	// ErrResultWindowExceeded indicates pagination reached Elasticsearch's
	// max_result_window. Deep paging past it needs search_after or a PIT, which
	// this backend does not use yet — the opaque cursor exists so adding them
	// will not change the interface.
	ErrResultWindowExceeded = platformerrors.New("search result window exceeded")
)

// Index implements our IndexManager interface.
func (sm *indexManager[T]) Index(ctx context.Context, id string, value any) error {
	ctx, op := sm.o11y.Begin(ctx)
	defer op.End()

	if sm.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	op.Set("id", id)
	op.Logger().Debug("adding to index")

	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	res, err := esapi.IndexRequest{
		Index:               sm.indexName,
		DocumentID:          id,
		Body:                bytes.NewReader(b),
		Timeout:             sm.indexOperationTimeout,
		Version:             nil,
		VersionType:         "",
		WaitForActiveShards: "",
		Pretty:              false,
		Human:               false,
		ErrorTrace:          false,
		FilterPath:          nil,
		Header:              nil,
	}.Do(ctx, sm.esClient)
	if err != nil {
		sm.circuitBreaker.Failed()
		return observability.PrepareError(err, op.Span(), "indexing value")
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			op.Acknowledge(closeErr, "closing response body")
		}
	}()

	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusOK {
		sm.circuitBreaker.Failed()
		return observability.PrepareError(platformerrors.New(res.String()), op.Span(), "indexing value")
	}

	sm.circuitBreaker.Succeeded()
	return nil
}

// search executes search queries.
func (sm *indexManager[T]) search(ctx context.Context, req textsearch.SearchRequest) (_ *textsearch.SearchResults[T], err error) {
	ctx, op := sm.o11y.Begin(ctx)
	defer op.End()

	if sm.circuitBreaker.CannotProceed() {
		return nil, circuitbreaking.ErrCircuitBroken
	}

	op.Set(keys.SearchQueryKey, req.Query)

	if req.Query == "" {
		return nil, ErrEmptyQueryProvided
	}

	from, err := textsearch.DecodeCursor(backendName, req.Cursor)
	if err != nil {
		return nil, observability.PrepareError(err, op.Span(), "decoding cursor")
	}

	size := textsearch.EffectiveLimit(req.Limit, maxSearchLimit)

	// from + size cannot exceed index.max_result_window (10,000 by default);
	// past that Elasticsearch rejects the request outright rather than
	// returning a short page, so the refusal is raised here with a name.
	if from+size > maxResultWindow {
		return nil, observability.PrepareError(ErrResultWindowExceeded, op.Span(), "paginating beyond the result window")
	}

	op.Set("search.from", from).Set("search.size", size)

	resultIDs := []*T{}
	q := searchQuery{
		Query: queryContainer{
			MultiMatch: multiMatchQuery{
				Query:  req.Query,
				Type:   "best_fields",
				Fields: []string{"*"},
			},
		},
		From: from,
		Size: size,
	}

	queryBody, err := json.Marshal(q)
	if err != nil {
		return nil, observability.PrepareError(err, op.Span(), "encodign search query")
	}

	res, err := sm.esClient.Search(
		sm.esClient.Search.WithContext(ctx),
		sm.esClient.Search.WithIndex(sm.indexName),
		sm.esClient.Search.WithBody(bytes.NewReader(queryBody)),
	)
	defer func() {
		if res != nil {
			if closeErr := res.Body.Close(); closeErr != nil {
				op.Acknowledge(closeErr, "closing response body")
			}
		}
	}()

	if err != nil {
		sm.circuitBreaker.Failed()
		return nil, observability.PrepareError(err, op.Span(), "querying elasticsearch successfully")
	}

	if res.IsError() {
		var e map[string]any
		if err = json.NewDecoder(res.Body).Decode(&e); err != nil {
			sm.circuitBreaker.Failed()
			return nil, observability.PrepareError(err, op.Span(), "invalid response from elasticsearch")
		}

		err = platformerrors.New(strings.Join(res.Warnings(), ", "))
		sm.circuitBreaker.Failed()
		return nil, observability.PrepareError(err, op.Span(), "querying elasticsearch")
	}

	var r esResponse
	if err = json.NewDecoder(res.Body).Decode(&r); err != nil {
		sm.circuitBreaker.Failed()
		return nil, observability.PrepareError(err, op.Span(), "decoding response")
	}

	for _, hit := range r.Hits.Hits {
		var c *T
		if err = json.Unmarshal(hit.Source, &c); err != nil {
			sm.circuitBreaker.Failed()
			return nil, observability.PrepareError(err, op.Span(), "decoding response")
		}
		resultIDs = append(resultIDs, c)
	}

	op.Set(keys.LengthKey, len(resultIDs))

	out := &textsearch.SearchResults[T]{Hits: resultIDs}

	// The next cursor is issued from the total, not from the page being short:
	// Elasticsearch can return fewer hits than requested and still have more,
	// so a short page is not the end of the result set.
	if next := from + len(resultIDs); len(resultIDs) > 0 && next < r.Hits.Total.Value && next < maxResultWindow {
		if out.NextCursor, err = textsearch.EncodeCursor(backendName, next); err != nil {
			return nil, observability.PrepareError(err, op.Span(), "encoding next cursor")
		}
	}

	sm.circuitBreaker.Succeeded()

	return out, nil
}

// Search implements our IndexSearcher interface.
func (sm *indexManager[T]) Search(ctx context.Context, req textsearch.SearchRequest) (*textsearch.SearchResults[T], error) {
	return sm.search(ctx, req)
}

// Wipe implements our IndexManager interface. It removes all documents from the
// index, leaving the index itself in place (matching the algolia/pgvector/qdrant
// backends), via a match-all delete-by-query with an immediate refresh.
func (sm *indexManager[T]) Wipe(ctx context.Context) error {
	ctx, op := sm.o11y.Begin(ctx)
	defer op.End()

	if sm.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	refresh := true
	res, err := esapi.DeleteByQueryRequest{
		Index:   []string{sm.indexName},
		Body:    strings.NewReader(`{"query":{"match_all":{}}}`),
		Refresh: &refresh,
	}.Do(ctx, sm.esClient)
	if err != nil {
		sm.circuitBreaker.Failed()
		return observability.PrepareError(err, op.Span(), "wiping index")
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			op.Acknowledge(closeErr, "closing response body")
		}
	}()

	if res.IsError() {
		sm.circuitBreaker.Failed()
		return observability.PrepareError(platformerrors.New(res.String()), op.Span(), "wiping index")
	}

	sm.circuitBreaker.Succeeded()
	return nil
}

// Delete implements our IndexManager interface.
func (sm *indexManager[T]) Delete(ctx context.Context, id string) error {
	ctx, op := sm.o11y.Begin(ctx)
	defer op.End()

	if sm.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	op.Set("id", id)

	res, err := esapi.DeleteRequest{
		Index:      sm.indexName,
		DocumentID: id,
	}.Do(ctx, sm.esClient)
	if err != nil {
		sm.circuitBreaker.Failed()
		return observability.PrepareError(err, op.Span(), "deleting from elasticsearch")
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			op.Acknowledge(closeErr, "closing response body")
		}
	}()

	// A delete targeting a document that does not exist returns 404 with
	// result "not_found". Treat that as success: the desired end state (document
	// absent) already holds, so Delete is idempotent for callers that retry or
	// delete speculatively.
	if res.StatusCode == http.StatusNotFound {
		op.Logger().Debug("document not found, treating delete as no-op")
		sm.circuitBreaker.Succeeded()
		return nil
	}

	// esapi only returns a non-nil err for transport-level failures; an HTTP error
	// status (401/500) surfaces on the response itself. Without this check a
	// failed delete would count as a success and leave the document in place.
	if res.IsError() {
		sm.circuitBreaker.Failed()
		return observability.PrepareError(platformerrors.New(res.String()), op.Span(), "deleting from elasticsearch")
	}

	op.Logger().Debug("removed from index")

	sm.circuitBreaker.Succeeded()
	return nil
}
