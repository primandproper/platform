package httpclient

import (
	"net/http"
	"testing"
	"time"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestCachedResponse_ToResponse(T *testing.T) {
	T.Parallel()

	stored := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)

	T.Run("rebuilds a response a caller reads exactly as the origin's", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       []byte(`{"keys":[]}`),
			OriginTime: stored,
		}

		req := newRequest(t.Context(), http.MethodGet, cacheURL, nil)
		resp := entry.toResponse(req, stored)

		test.EqOp(t, http.StatusOK, resp.StatusCode)
		test.EqOp(t, "200 OK", resp.Status)
		test.EqOp(t, "application/json", resp.Header.Get("Content-Type"))
		test.EqOp(t, int64(len(entry.Body)), resp.ContentLength)
		test.EqOp(t, req, resp.Request)
		test.EqOp(t, `{"keys":[]}`, readBody(t, resp))
	})

	T.Run("Age is measured from the origin, not from residency here", func(t *testing.T) {
		t.Parallel()

		// OriginTime already folds in whatever age the response arrived
		// carrying, so an entry stored ten seconds ago from a response that was
		// already two minutes old reports one hundred thirty seconds.
		entry := &CachedResponse{Header: http.Header{}, OriginTime: stored.Add(-2 * time.Minute)}

		resp := entry.toResponse(newRequest(t.Context(), http.MethodGet, cacheURL, nil), stored.Add(10*time.Second))

		test.EqOp(t, "130", resp.Header.Get("Age"))
		must.NoError(t, resp.Body.Close())
	})

	T.Run("an inherited Age is replaced rather than replayed", func(t *testing.T) {
		t.Parallel()

		// Replaying the stored value would understate the age by however long
		// the entry has been sitting here.
		entry := &CachedResponse{Header: http.Header{"Age": []string{"5"}}, OriginTime: stored}

		resp := entry.toResponse(newRequest(t.Context(), http.MethodGet, cacheURL, nil), stored.Add(time.Minute))

		test.EqOp(t, "60", resp.Header.Get("Age"))
		must.NoError(t, resp.Body.Close())
	})

	T.Run("the caller cannot reach into the cache through the response", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			OriginTime: stored,
		}

		resp := entry.toResponse(newRequest(t.Context(), http.MethodGet, cacheURL, nil), stored)
		resp.Header.Set("Content-Type", "text/plain")
		must.NoError(t, resp.Body.Close())

		// http.Client's own redirect handling mutates response headers, so this
		// is not a hypothetical.
		test.EqOp(t, "application/json", entry.Header.Get("Content-Type"))
	})
}

func TestCachedResponse_Clone(T *testing.T) {
	T.Parallel()

	T.Run("a clone's header and vary are its own", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{
			Header: header("ETag", `"v1"`),
			Vary:   map[string]string{"Accept": "application/json"},
		}

		cloned := entry.clone()
		cloned.Header.Set("ETag", `"v2"`)
		cloned.Vary["Accept"] = "text/xml"

		// cache/memory stores values directly, so a Get hands back the very
		// pointer a Set stored. Revalidation merges a 304 into what it read,
		// and without this that merge would be a write into the cache — and,
		// with two requests for one URL in flight, into a map another goroutine
		// is reading.
		test.EqOp(t, `"v1"`, entry.Header.Get("ETag"))
		test.EqOp(t, "application/json", entry.Vary["Accept"])
	})

	T.Run("a clone of an entry with no header still has one", func(t *testing.T) {
		t.Parallel()

		cloned := (&CachedResponse{}).clone()

		must.NotNil(t, cloned.Header)
		test.Nil(t, cloned.Vary)
	})
}

func TestCachedResponse_Refresh(T *testing.T) {
	T.Parallel()

	T.Run("adopts the validating response's fields", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{
			Header:     header("ETag", `"v1"`, "Content-Type", "application/json"),
			OriginTime: time.Unix(1, 0),
		}

		refreshed := time.Unix(1_700_000_000, 0)
		entry.refresh(http.Header{
			"Cache-Control": []string{"max-age=600"},
			"Content-Type":  []string{"application/jwk-set+json"},
		}, refreshed)

		test.EqOp(t, "max-age=600", entry.Header.Get("Cache-Control"))
		test.EqOp(t, "application/jwk-set+json", entry.Header.Get("Content-Type"))
		test.EqOp(t, refreshed, entry.OriginTime)

		// Not restated by the 304 and therefore kept: this is how a bare 304
		// leaves a stored validator intact.
		test.EqOp(t, `"v1"`, entry.Header.Get("ETag"))
	})

	T.Run("a Content-Length on a 304 describes a body this entry does not hold", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{
			Header: http.Header{"Content-Length": []string{"11"}},
			Body:   []byte(`{"keys":[]}`),
		}

		entry.refresh(http.Header{"Content-Length": []string{"4096"}}, time.Unix(1_700_000_000, 0))

		test.EqOp(t, "11", entry.Header.Get("Content-Length"))
	})

	T.Run("hop-by-hop fields are not adopted", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{Header: http.Header{}}

		entry.refresh(http.Header{"Connection": []string{"close"}}, time.Unix(1_700_000_000, 0))

		// They described the connection the 304 arrived on, which is not the
		// one this entry will be replayed over.
		test.EqOp(t, "", entry.Header.Get("Connection"))
	})
}

func TestCachedResponse_Matches(T *testing.T) {
	T.Parallel()

	T.Run("an entry stored against no Vary matches everything", func(t *testing.T) {
		t.Parallel()

		req := newRequest(t.Context(), http.MethodGet, cacheURL, nil)
		req.Header.Set("Accept", "text/xml")

		test.True(t, (&CachedResponse{}).matches(req))
	})

	T.Run("an absent header matches an entry stored without one", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{Vary: map[string]string{"Accept": ""}}

		test.True(t, entry.matches(newRequest(t.Context(), http.MethodGet, cacheURL, nil)))
	})

	T.Run("a differing value does not match", func(t *testing.T) {
		t.Parallel()

		entry := &CachedResponse{Vary: map[string]string{"Accept": "application/json"}}

		req := newRequest(t.Context(), http.MethodGet, cacheURL, nil)
		req.Header.Set("Accept", "text/xml")

		test.False(t, entry.matches(req))
	})
}

func TestStorableHeader(T *testing.T) {
	T.Parallel()

	T.Run("drops the fields that described one connection", func(t *testing.T) {
		t.Parallel()

		stored := storableHeader(http.Header{
			"Content-Type":      []string{"application/json"},
			"Connection":        []string{"keep-alive"},
			"Transfer-Encoding": []string{"chunked"},
			"Keep-Alive":        []string{"timeout=5"},
		})

		test.EqOp(t, "application/json", stored.Get("Content-Type"))
		test.EqOp(t, "", stored.Get("Connection"))
		test.EqOp(t, "", stored.Get("Transfer-Encoding"))
		test.EqOp(t, "", stored.Get("Keep-Alive"))
	})

	T.Run("the result is not the header it was given", func(t *testing.T) {
		t.Parallel()

		original := http.Header{"Connection": []string{"keep-alive"}}

		test.EqOp(t, "", storableHeader(original).Get("Connection"))
		test.EqOp(t, "keep-alive", original.Get("Connection"))
	})
}
