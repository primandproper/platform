package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/primandproper/platform-go/v14/observability"
	"github.com/primandproper/platform-go/v14/routing"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// tagging builds a middleware that appends name to a shared slice on the way in
// and name+":out" on the way out, which is how a test reads the order a chain
// actually applied rather than the order it was written in.
func tagging(order *[]string, name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			*order = append(*order, name)
			next.ServeHTTP(res, req)
			*order = append(*order, name+":out")
		})
	}
}

func serve(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody))

	return res
}

func TestChain(T *testing.T) {
	T.Parallel()

	T.Run("mws[0] is outermost, matching chi's Use", func(t *testing.T) {
		t.Parallel()

		// This ordering is the whole reason the helper exists: it lets the
		// backends that chain by hand and the one that calls chi.Router.Use
		// agree on what a middleware slice means.
		var order []string

		h := Chain(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
			tagging(&order, "first"),
			tagging(&order, "second"),
		)

		serve(t, h, http.MethodGet, "/")

		test.Eq(t, []string{"first", "second", "handler", "second:out", "first:out"}, order)
	})

	T.Run("nil entries are skipped", func(t *testing.T) {
		t.Parallel()

		var order []string

		mws := []func(http.Handler) http.Handler{nil, tagging(&order, "only"), nil}

		h := Chain(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "handler") }),
			mws...,
		)

		serve(t, h, http.MethodGet, "/")

		test.Eq(t, []string{"only", "handler", "only:out"}, order)
	})

	T.Run("no middleware returns the handler unchanged", func(t *testing.T) {
		t.Parallel()

		var served bool
		h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { served = true })

		serve(t, Chain(h), http.MethodGet, "/")

		test.True(t, served)
	})
}

func TestConvert(T *testing.T) {
	T.Parallel()

	T.Run("drops nil entries so optional middleware needs no guard", func(t *testing.T) {
		t.Parallel()

		first := func(h http.Handler) http.Handler { return h }
		second := func(h http.Handler) http.Handler { return h }

		out := Convert(routing.Middleware(first), nil, routing.Middleware(second))

		test.SliceLen(t, 2, out)
	})

	T.Run("no input yields an empty, non-nil slice", func(t *testing.T) {
		t.Parallel()

		out := Convert()

		test.NotNil(t, out)
		test.SliceEmpty(t, out)
	})

	T.Run("the converted middleware still wraps", func(t *testing.T) {
		t.Parallel()

		var order []string

		out := Convert(routing.Middleware(tagging(&order, "converted")))
		must.SliceLen(t, 1, out)

		serve(t, Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), out...), http.MethodGet, "/")

		test.Eq(t, []string{"converted", "converted:out"}, order)
	})
}

func TestColonParams(T *testing.T) {
	T.Parallel()

	for _, tc := range []struct {
		name     string
		pattern  string
		expected string
	}{
		{name: "one placeholder", pattern: "/users/{id}", expected: "/users/:id"},
		{name: "several placeholders", pattern: "/users/{id}/things/{thingID}", expected: "/users/:id/things/:thingID"},
		{name: "a placeholder mid-segment", pattern: "/files/{name}.json", expected: "/files/:name.json"},
		{name: "no placeholders is unchanged", pattern: "/healthz", expected: "/healthz"},
		{name: "the root is unchanged", pattern: "/", expected: "/"},
		{name: "an empty placeholder is not a placeholder", pattern: "/users/{}", expected: "/users/{}"},
		{name: "a brace spanning a separator is not a placeholder", pattern: "/a/{b/c}", expected: "/a/{b/c}"},
	} {
		T.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			test.EqOp(t, tc.expected, ColonParams(tc.pattern))
		})
	}
}

func TestRequestID(T *testing.T) {
	T.Parallel()

	T.Run("reads the ID chi's middleware assigned", func(t *testing.T) {
		t.Parallel()

		// Read from chi's context key regardless of backend, because every
		// backend in this module installs chi's RequestID middleware.
		var got string

		h := chimiddleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
			got = RequestID(req)
		}))

		serve(t, h, http.MethodGet, "/")

		test.NotEqOp(t, "", got)
	})

	T.Run("an unassigned request has no ID rather than a fabricated one", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)

		test.EqOp(t, "", RequestID(req))
	})
}

func TestRecovery(T *testing.T) {
	T.Parallel()

	T.Run("a panicking handler becomes a 500 rather than a severed connection", func(t *testing.T) {
		t.Parallel()

		h := Recovery(observability.NewRecordingObserver())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("handler blew up")
		}))

		res := serve(t, h, http.MethodGet, "/")

		test.EqOp(t, http.StatusInternalServerError, res.Code)
	})

	T.Run("a panic with an error value is recovered too", func(t *testing.T) {
		t.Parallel()

		h := Recovery(observability.NewRecordingObserver())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrBodyNotAllowed)
		}))

		res := serve(t, h, http.MethodGet, "/")

		test.EqOp(t, http.StatusInternalServerError, res.Code)
	})

	T.Run("http.ErrAbortHandler is re-panicked so the connection is still aborted", func(t *testing.T) {
		t.Parallel()

		// The standard library uses this panic to mean "drop the connection
		// without a response". Swallowing it would turn a deliberate abort into
		// a 500.
		h := Recovery(observability.NewRecordingObserver())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))

		defer func() {
			rec := recover()
			must.NotNil(t, rec)

			err, ok := rec.(error)
			must.True(t, ok)
			test.ErrorIs(t, err, http.ErrAbortHandler)
		}()

		serve(t, h, http.MethodGet, "/")
		t.Error("ErrAbortHandler was swallowed instead of re-panicked")
	})

	T.Run("a handler that does not panic is left alone", func(t *testing.T) {
		t.Parallel()

		h := Recovery(observability.NewRecordingObserver())(http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) {
			res.WriteHeader(http.StatusTeapot)
		}))

		test.EqOp(t, http.StatusTeapot, serve(t, h, http.MethodGet, "/").Code)
	})
}

func TestStandard(T *testing.T) {
	T.Parallel()

	T.Run("returns the ordered stack and every entry wraps", func(t *testing.T) {
		t.Parallel()

		cfg := &StackConfig{ServiceName: "test", ValidDomains: []string{"example.com"}}

		mws := Standard(observability.NewRecordingObserver(), cfg)
		must.SliceLen(t, 8, mws)

		for i, mw := range mws {
			must.NotNil(t, mw, must.Sprintf("middleware %d", i))
		}
	})

	T.Run("the assembled stack serves a request", func(t *testing.T) {
		t.Parallel()

		// Chained rather than asserted one by one: the ordering constraint the
		// doc comment describes — request ID and real IP ahead of the
		// observability middleware — only means anything end to end.
		cfg := &StackConfig{ServiceName: "test", ValidDomains: []string{"example.com"}}

		var seenRequestID string

		h := Chain(
			http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				seenRequestID = RequestID(req)
				res.WriteHeader(http.StatusNoContent)
			}),
			Standard(observability.NewRecordingObserver(), cfg)...,
		)

		res := serve(t, h, http.MethodGet, "/things")

		test.EqOp(t, http.StatusNoContent, res.Code)
		test.NotEqOp(t, "", seenRequestID)
	})

	T.Run("a health check passes through untraced", func(t *testing.T) {
		t.Parallel()

		cfg := &StackConfig{ServiceName: "test"}

		obs := observability.NewRecordingObserver()
		h := Chain(
			http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) { res.WriteHeader(http.StatusOK) }),
			Standard(obs, cfg)...,
		)

		test.EqOp(t, http.StatusOK, serve(t, h, http.MethodGet, "/_ops_/live").Code)
		test.SliceEmpty(t, obs.Operations)
	})

	T.Run("silencing route logging still opens the span", func(t *testing.T) {
		t.Parallel()

		cfg := &StackConfig{ServiceName: "test", SilenceRouteLogging: true}

		obs := observability.NewRecordingObserver()
		h := Chain(
			http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) { res.WriteHeader(http.StatusOK) }),
			Standard(obs, cfg)...,
		)

		test.EqOp(t, http.StatusOK, serve(t, h, http.MethodGet, "/things").Code)
		test.SliceLen(t, 1, obs.Operations)
	})
}

func TestCORS(T *testing.T) {
	T.Parallel()

	// preflight asks whether origin would be allowed, reading the answer off the
	// Access-Control-Allow-Origin header the middleware writes.
	preflight := func(t *testing.T, mw func(http.Handler) http.Handler, origin string) string {
		t.Helper()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "https://api.example.com/things", http.NoBody)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)

		res := httptest.NewRecorder()
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(res, req)

		return res.Header().Get("Access-Control-Allow-Origin")
	}

	T.Run("an https origin in validDomains is allowed", func(t *testing.T) {
		t.Parallel()

		mw := CORS(observability.NewRecordingObserver(), []string{"example.com"}, false)

		test.EqOp(t, "https://example.com", preflight(t, mw, "https://example.com"))
	})

	T.Run("a plaintext origin on an allowed host is refused", func(t *testing.T) {
		t.Parallel()

		// Credentials are allowed on this stack, so accepting http here would
		// accept credentialed requests from a plaintext origin on the very
		// hostname the allowlist is meant to protect.
		mw := CORS(observability.NewRecordingObserver(), []string{"example.com"}, false)

		test.EqOp(t, "", preflight(t, mw, "http://example.com"))
	})

	T.Run("an https origin outside validDomains is refused", func(t *testing.T) {
		t.Parallel()

		mw := CORS(observability.NewRecordingObserver(), []string{"example.com"}, false)

		test.EqOp(t, "", preflight(t, mw, "https://evil.example.net"))
	})

	T.Run("localhost over http needs the flag", func(t *testing.T) {
		t.Parallel()

		off := CORS(observability.NewRecordingObserver(), nil, false)
		on := CORS(observability.NewRecordingObserver(), nil, true)

		test.EqOp(t, "", preflight(t, off, "http://localhost:3000"))
		test.EqOp(t, "http://localhost:3000", preflight(t, on, "http://localhost:3000"))
		test.EqOp(t, "http://127.0.0.1:3000", preflight(t, on, "http://127.0.0.1:3000"))
	})

	T.Run("an unparseable origin is refused", func(t *testing.T) {
		t.Parallel()

		mw := CORS(observability.NewRecordingObserver(), []string{"example.com"}, true)

		test.EqOp(t, "", preflight(t, mw, "://not a url"))
	})

	T.Run("credentials are allowed and no empty expose-headers is emitted", func(t *testing.T) {
		t.Parallel()

		// ExposedHeaders is nil rather than []string{""}, which used to emit an
		// empty Access-Control-Expose-Headers on every response.
		mw := CORS(observability.NewRecordingObserver(), []string{"example.com"}, false)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "https://api.example.com/things", http.NoBody)
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)

		res := httptest.NewRecorder()
		mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(res, req)

		test.EqOp(t, "true", res.Header().Get("Access-Control-Allow-Credentials"))

		_, present := res.Header()["Access-Control-Expose-Headers"]
		test.False(t, present)
	})

	T.Run("the advertised methods are the ones the stack serves", func(t *testing.T) {
		t.Parallel()

		// The middleware echoes back the single method the preflight asked
		// about, so each is asked for in turn rather than read off one response.
		mw := CORS(observability.NewRecordingObserver(), []string{"example.com"}, false)

		ask := func(method string) string {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "https://api.example.com/things", http.NoBody)
			req.Header.Set("Origin", "https://example.com")
			req.Header.Set("Access-Control-Request-Method", method)

			res := httptest.NewRecorder()
			mw(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(res, req)

			return res.Header().Get("Access-Control-Allow-Methods")
		}

		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions} {
			test.EqOp(t, method, ask(method), test.Sprintf("method %q should be allowed", method))
		}

		// TRACE is not on the list, and a method that is not on the list is not
		// advertised back.
		test.EqOp(t, "", ask(http.MethodTrace))
	})
}

func TestOtelMiddleware(T *testing.T) {
	T.Parallel()

	T.Run("serves the request through otelhttp", func(t *testing.T) {
		t.Parallel()

		mw := otelMiddleware(&StackConfig{ServiceName: "test"})

		h := mw(http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) { res.WriteHeader(http.StatusTeapot) }))

		test.EqOp(t, http.StatusTeapot, serve(t, h, http.MethodGet, "/things").Code)
	})

	T.Run("probes are filtered out", func(t *testing.T) {
		t.Parallel()

		// Load balancers and scrapers hit these constantly; tracing them buries
		// the requests somebody is actually looking for.
		mw := otelMiddleware(&StackConfig{ServiceName: "test"})

		h := mw(http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) { res.WriteHeader(http.StatusOK) }))

		test.EqOp(t, http.StatusOK, serve(t, h, http.MethodGet, "/_ops_/live").Code)
	})

	T.Run("a nil tracer provider is resolved rather than dereferenced", func(t *testing.T) {
		t.Parallel()

		mw := otelMiddleware(&StackConfig{ServiceName: "test", TracerProvider: nil})

		h := mw(http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) { res.WriteHeader(http.StatusOK) }))

		test.EqOp(t, http.StatusOK, serve(t, h, http.MethodGet, "/things").Code)
	})
}
