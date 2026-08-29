package oauth2server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/primandproper/platform-go/v13/authentication/oauth2server"
	"github.com/primandproper/platform-go/v13/authentication/oauth2server/memory"
	"github.com/primandproper/platform-go/v13/ratelimiting"
	ratelimitinghttp "github.com/primandproper/platform-go/v13/ratelimiting/http"
	"github.com/primandproper/platform-go/v13/routing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// refusingGate is a stand-in for whatever gate a deployment supplies: it counts
// what it saw and answers 429 without calling through.
//
// A stub rather than a real rate limiter for most of these, deliberately. What
// is under test is that the seam is wired to every way of routing /register and
// to nothing else; a real limiter would make each case also a test of a token
// bucket, and the one case that should be is the one below that uses one.
func refusingGate(seen *atomic.Int64) routing.Middleware {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) {
			seen.Add(1)
			res.WriteHeader(http.StatusTooManyRequests)
		})
	}
}

// countingPolicy records how many registrations reached the endpoint's vetting,
// which is the closest thing to "did the handler run at all".
type countingPolicy struct {
	consulted atomic.Int64
}

func (p *countingPolicy) AllowRegistration(ctx context.Context, req *oauth2server.RegistrationRequest) error {
	p.consulted.Add(1)

	return oauth2server.DefaultRegistrationPolicy.AllowRegistration(ctx, req)
}

// postRegister sends one registration to an arbitrary handler and reports the
// status, so the cases can compare the three ways of routing the endpoint
// without a harness each.
func postRegister(t *testing.T, handler http.Handler) int {
	t.Helper()

	front := httptest.NewServer(handler)
	t.Cleanup(front.Close)

	body := strings.NewReader(`{"redirect_uris":["` + testRedirectURI + `"]}`)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, front.URL+oauth2server.PathRegister, body)
	must.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	res, err := front.Client().Do(req)
	must.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	return res.StatusCode
}

func TestWithRegistrationLimiter(T *testing.T) {
	T.Parallel()

	T.Run("a refused registration never reaches the endpoint", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Int64

		policy := &countingPolicy{}

		h := newHarness(t,
			oauth2server.WithRegistrationPolicy(policy),
			oauth2server.WithRegistrationLimiter(refusingGate(&seen)))

		reg := h.register(map[string]any{"redirect_uris": []string{testRedirectURI}})

		test.EqOp(t, http.StatusTooManyRequests, reg.status)
		test.EqOp(t, "", reg.ClientID)

		// The gate ran and the handler did not, which is the whole point of
		// putting it in front rather than inside: a refusal costs no store
		// write and no policy evaluation.
		test.EqOp(t, int64(1), seen.Load())
		test.EqOp(t, int64(0), policy.consulted.Load())
	})

	T.Run("an admitted registration proceeds as it always did", func(t *testing.T) {
		t.Parallel()

		var admitted atomic.Int64

		h := newHarness(t, oauth2server.WithRegistrationLimiter(
			func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
					admitted.Add(1)
					next.ServeHTTP(res, req)
				})
			}))

		reg := h.register(map[string]any{"redirect_uris": []string{testRedirectURI}})

		must.EqOp(t, http.StatusCreated, reg.status)
		test.NotEq(t, "", reg.ClientID)
		test.NotEq(t, "", reg.ClientSecret)
		test.EqOp(t, int64(1), admitted.Load())
	})

	T.Run("a nil gate leaves the endpoint exactly as it was", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t, oauth2server.WithRegistrationLimiter(nil))

		reg := h.register(map[string]any{"redirect_uris": []string{testRedirectURI}})

		test.EqOp(t, http.StatusCreated, reg.status)
		test.NotEq(t, "", reg.ClientID)
	})

	T.Run("the gate reaches every way of routing the endpoint", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Int64

		server, err := oauth2server.NewServer(testIssuer, memory.NewStore(), &passwordAuthenticator{},
			oauth2server.WithRegistrationLimiter(refusingGate(&seen)))
		must.NoError(t, err)

		// Handler, Mount, and the endpoint routed by hand. The option is an
		// option rather than a Mount argument so that all three are bounded by
		// one construction call; a gate that only survived one of them would be
		// the router-shaped answer this replaced.
		router := newTestRouter(t)
		server.Mount(router)
		must.NoError(t, router.Err())

		byHand := http.NewServeMux()
		byHand.Handle("POST "+oauth2server.PathRegister, server.RegisterHandler())

		for name, handler := range map[string]http.Handler{
			"Handler": server.Handler(),
			"Mount":   router.Handler(),
			"by hand": byHand,
		} {
			test.EqOp(t, http.StatusTooManyRequests, postRegister(t, handler),
				test.Sprintf("routed via %s", name))
		}

		test.EqOp(t, int64(3), seen.Load())
	})

	T.Run("the gate runs before the disabled-registration refusal", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Int64

		server, err := oauth2server.NewServer(testIssuer, memory.NewStore(), &passwordAuthenticator{},
			oauth2server.WithDynamicRegistration(false),
			oauth2server.WithRegistrationLimiter(refusingGate(&seen)))
		must.NoError(t, err)

		byHand := http.NewServeMux()
		byHand.Handle("POST "+oauth2server.PathRegister, server.RegisterHandler())

		// 429 rather than 404: a caller hammering an endpoint that is turned
		// off is exactly what a bound is for, and answering the 404 first would
		// mean the one deployment with no registration table to protect is the
		// one that cannot protect anything.
		test.EqOp(t, http.StatusTooManyRequests, postRegister(t, byHand))
		test.EqOp(t, int64(1), seen.Load())
	})

	T.Run("no other endpoint is behind it", func(t *testing.T) {
		t.Parallel()

		var seen atomic.Int64

		h := newHarness(t, oauth2server.WithRegistrationLimiter(refusingGate(&seen)))

		// The discovery document, which every client fetches and which writes
		// nothing. A gate handed to Mount would have covered this too.
		res := h.get(oauth2server.PathAuthorizationServerMetadata)
		test.EqOp(t, http.StatusOK, res.StatusCode)

		test.EqOp(t, int64(0), seen.Load())
	})

	T.Run("a real rate limiter bounds the endpoint", func(t *testing.T) {
		t.Parallel()

		limiter, err := ratelimiting.NewInMemoryRateLimiter(1, 1)
		must.NoError(t, err)
		t.Cleanup(func() { must.NoError(t, limiter.Close()) })

		gate, err := ratelimitinghttp.NewMiddleware(limiter, ratelimitinghttp.KeyByRemoteAddr())
		must.NoError(t, err)

		h := newHarness(t, oauth2server.WithRegistrationLimiter(gate))

		first := h.register(map[string]any{"redirect_uris": []string{testRedirectURI}})
		must.EqOp(t, http.StatusCreated, first.status)

		// A burst of one, so the second registration from the same address is
		// the one the bucket has nothing left for.
		second := h.register(map[string]any{"redirect_uris": []string{testRedirectURI}})
		test.EqOp(t, http.StatusTooManyRequests, second.status)
		test.EqOp(t, "", second.ClientID)
	})
}
