package algolia

import (
	"context"
	"encoding/json"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability/keys"
	textsearch "github.com/primandproper/platform-go/v10/search/text"

	"github.com/algolia/algoliasearch-client-go/v3/algolia/opt"
)

const (
	objectIDKey = "objectID"
	idKey       = "id"

	// backendName tags cursors this package issues, so one cannot be resumed
	// against a different backend.
	backendName = "algolia"

	// maxSearchLimit caps a single page. Algolia's own ceiling for
	// hitsPerPage is 1000, which is far past useful.
	maxSearchLimit = 200
)

var (
	// ErrEmptyQueryProvided indicates an empty query was provided as input.
	ErrEmptyQueryProvided = platformerrors.New("empty search query provided")
)

// Index implements our indexManager interface.
func (m *indexManager[T]) Index(ctx context.Context, id string, value any) error {
	_, op := m.o11y.Begin(ctx)
	defer op.End()

	if m.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	op.Set(idKey, id)
	op.Logger().Debug("adding to index")

	jsonEncoded, err := json.Marshal(value)
	if err != nil {
		return err
	}

	var newValue map[string]any
	if unmarshalErr := json.Unmarshal(jsonEncoded, &newValue); unmarshalErr != nil {
		return unmarshalErr
	}

	// we make a huge, albeit safe assumption here.
	newValue[objectIDKey] = newValue[idKey]
	delete(newValue, idKey)

	if _, err = m.client.SaveObject(newValue); err != nil {
		m.circuitBreaker.Failed()
		return err
	}

	m.circuitBreaker.Succeeded()
	return nil
}

// Search implements our IndexSearcher interface.
func (m *indexManager[T]) Search(ctx context.Context, req textsearch.SearchRequest) (*textsearch.SearchResults[T], error) {
	_, op := m.o11y.Begin(ctx)
	defer op.End()

	if m.circuitBreaker.CannotProceed() {
		return nil, circuitbreaking.ErrCircuitBroken
	}

	op.Set(keys.SearchQueryKey, req.Query)

	if req.Query == "" {
		return nil, ErrEmptyQueryProvided
	}

	// Algolia paginates by page number, not document offset, so that is what
	// this backend's cursor carries.
	page, err := textsearch.DecodeCursor(backendName, req.Cursor)
	if err != nil {
		m.circuitBreaker.Failed()

		return nil, err
	}

	hitsPerPage := textsearch.EffectiveLimit(req.Limit, maxSearchLimit)

	op.Set("search.page", page).Set("search.hitsPerPage", hitsPerPage)

	res, searchErr := m.client.Search(req.Query, opt.Page(page), opt.HitsPerPage(hitsPerPage))
	if searchErr != nil {
		m.circuitBreaker.Failed()
		return nil, searchErr
	}

	results := []*T{}
	for _, hit := range res.Hits {
		var x *T

		// we make the same assumption here, sort of
		if _, ok := hit[objectIDKey]; ok {
			hit[idKey] = hit[objectIDKey]
			delete(hit, objectIDKey)
		}

		var encodedAsJSON []byte
		encodedAsJSON, marshalErr := json.Marshal(hit)
		if marshalErr != nil {
			return nil, marshalErr
		}

		if unmarshalErr := json.Unmarshal(encodedAsJSON, &x); unmarshalErr != nil {
			return nil, unmarshalErr
		}

		results = append(results, x)
	}

	op.Set(keys.LengthKey, len(results))
	op.Logger().Debug("search performed")

	out := &textsearch.SearchResults[T]{Hits: results}

	// NbPages is authoritative for whether another page exists; a short page is
	// not the signal, since Algolia can return fewer hits than hitsPerPage and
	// still have more pages behind it.
	if next := page + 1; len(results) > 0 && next < res.NbPages {
		if out.NextCursor, err = textsearch.EncodeCursor(backendName, next); err != nil {
			m.circuitBreaker.Failed()

			return nil, err
		}
	}

	m.circuitBreaker.Succeeded()

	return out, nil
}

// Delete implements our indexManager interface.
func (m *indexManager[T]) Delete(ctx context.Context, id string) error {
	_, op := m.o11y.Begin(ctx)
	defer op.End()

	if m.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	op.Set(idKey, id)

	if _, err := m.client.DeleteObject(id); err != nil {
		m.circuitBreaker.Failed()
		return err
	}

	op.Logger().Debug("removed from index")

	m.circuitBreaker.Succeeded()
	return nil
}

// Wipe implements our indexManager interface.
func (m *indexManager[T]) Wipe(ctx context.Context) error {
	_, op := m.o11y.Begin(ctx)
	defer op.End()

	if m.circuitBreaker.CannotProceed() {
		return circuitbreaking.ErrCircuitBroken
	}

	if _, err := m.client.ClearObjects(); err != nil {
		m.circuitBreaker.Failed()
		return err
	}

	m.circuitBreaker.Succeeded()
	return nil
}
