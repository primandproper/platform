package http

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// BenchmarkMiddleware_NoKey is the number that decides whether this can ever be
// installed globally: every request that does not opt in pays it.
//
// Baseline runs the same handler through the same harness without the
// middleware, because request and recorder construction dominate at this scale
// and an absolute number would read as far worse than the middleware actually
// is. The delta between the two is the cost; each alone is mostly harness.
func BenchmarkMiddleware_NoKey(b *testing.B) {
	handler := okHandler()
	body := strings.Repeat("a", 1024)

	run := func(b *testing.B, h http.Handler) {
		b.Helper()

		for b.Loop() {
			req := httptest.NewRequest(http.MethodPost, "/charges", strings.NewReader(body))
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	}

	b.Run("Baseline", func(b *testing.B) {
		run(b, handler)
	})

	b.Run("Wrapped", func(b *testing.B) {
		run(b, wrap(b, handler, newTestManager(b)))
	})
}

func BenchmarkMiddleware(b *testing.B) {
	const body = `{"amount":10}`

	// Replay is what a duplicate costs: fingerprint plus one store read, no
	// handler, no lock.
	b.Run("Replay", func(b *testing.B) {
		wrapped := wrap(b, okHandler(), newTestManager(b))

		seed := httptest.NewRequest(http.MethodPost, "/charges", strings.NewReader(body))
		seed.Header.Set(HeaderName, testKey)
		wrapped.ServeHTTP(httptest.NewRecorder(), seed)

		for b.Loop() {
			req := httptest.NewRequest(http.MethodPost, "/charges", strings.NewReader(body))
			req.Header.Set(HeaderName, testKey)
			wrapped.ServeHTTP(httptest.NewRecorder(), req)
		}
	})

	// Execute is a first-time request: lock, claim, handler, record.
	b.Run("Execute", func(b *testing.B) {
		wrapped := wrap(b, okHandler(), newTestManager(b))

		var i int
		for b.Loop() {
			i++
			req := httptest.NewRequest(http.MethodPost, "/charges", strings.NewReader(body))
			req.Header.Set(HeaderName, strconv.Itoa(i))
			wrapped.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
}

// BenchmarkFingerprint prices the hash against body size, since that is the
// part that scales with the request rather than with the route.
func BenchmarkFingerprint(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		b.Run(strconv.Itoa(size>>10)+"KiB", func(b *testing.B) {
			body := []byte(strings.Repeat("a", size))
			req := httptest.NewRequest(http.MethodPost, "/charges?b=2&a=1", http.NoBody)

			b.SetBytes(int64(size))
			for b.Loop() {
				_ = fingerprint(req, "user-1", body)
			}
		})
	}
}
