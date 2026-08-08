package httpclient

import (
	"net/http"
	"testing"

	"github.com/primandproper/platform-go/v10/circuitbreaking"
	"github.com/primandproper/platform-go/v10/ratelimiting"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestResilienceNesting(T *testing.T) {
	T.Parallel()

	T.Run("wraps observability over breaker over retry over rate limit over the base", func(t *testing.T) {
		t.Parallel()

		base := stubRoundTripper{}

		client := newClient(t,
			WithTransport(base),
			// Deliberately named in an order that does not match the nesting:
			// the arrangement is a property of the middlewares, not of the
			// caller's typing.
			WithRateLimit(&stubLimiter{allow: func(string) (bool, error) { return true, nil }}),
			WithCircuitBreaker(closedBreaker()),
			WithRetryPolicy(&immediatePolicy{attempts: 2}),
		)

		// Outermost, so the span it opens covers the breaker's rejections as
		// well as the attempts underneath them.
		observed, ok := client.Transport.(*observedTransport)
		must.True(t, ok)

		breaker, ok := observed.base.(*breakerTransport)
		must.True(t, ok)

		retrier, ok := breaker.base.(*retryTransport)
		must.True(t, ok)

		limiter, ok := retrier.base.(*rateLimitTransport)
		must.True(t, ok)

		_, ok = limiter.base.(stubRoundTripper)
		test.True(t, ok)
	})

	T.Run("tracing sits below the resilience layers", func(t *testing.T) {
		t.Parallel()

		client := newClient(t,
			WithTransport(stubRoundTripper{}),
			WithTracing(true),
			WithRetryPolicy(&immediatePolicy{attempts: 2}),
		)

		observed, ok := client.Transport.(*observedTransport)
		must.True(t, ok)

		retrier, ok := observed.base.(*retryTransport)
		must.True(t, ok)

		// Each attempt gets its own client span rather than one span stretched
		// over the whole loop.
		_, ok = retrier.base.(stubRoundTripper)
		test.False(t, ok)
	})

	T.Run("a client with no resilience layers is not wrapped", func(t *testing.T) {
		t.Parallel()

		// Nothing to describe that otelhttp does not already describe, so the
		// logical-request span would only duplicate the per-attempt one.
		client := newClient(t, WithTransport(stubRoundTripper{}))

		_, ok := client.Transport.(*observedTransport)
		test.False(t, ok)
	})
}

func TestResilienceComposition(T *testing.T) {
	T.Parallel()

	T.Run("every retried attempt spends a token", func(t *testing.T) {
		t.Parallel()

		limiter := &stubLimiter{allow: func(string) (bool, error) { return true, nil }}

		var calls int
		client := newClient(t,
			WithTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				calls++

				return response(http.StatusServiceUnavailable, "down"), nil
			})),
			WithRateLimit(limiter),
			WithRetryPolicy(&immediatePolicy{attempts: 3}),
		)

		resp, err := get(t.Context(), client, "http://example.com/thing")
		must.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		// The whole point of putting the limiter below the retry loop: a provider
		// counting requests against a documented budget counts all three of these.
		test.EqOp(t, 3, calls)
		test.SliceLen(t, 3, limiter.keys)
	})

	T.Run("an open circuit is not retried", func(t *testing.T) {
		t.Parallel()

		client := newClient(t,
			WithTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				t.Error("the request should never have reached the wire")

				return nil, nil
			})),
			WithCircuitBreaker(openBreaker()),
			WithRetryPolicy(&immediatePolicy{attempts: 3}),
		)

		// Failing fast three times with backoff in between is not failing fast,
		// which is why the breaker is outermost.
		resp, err := get(t.Context(), client, "http://example.com/thing")
		test.Nil(t, resp)
		must.Error(t, err)
		test.ErrorIs(t, err, circuitbreaking.ErrCircuitBroken)
	})

	T.Run("a rate-limited attempt is retried rather than failing the request", func(t *testing.T) {
		t.Parallel()

		var asked int
		limiter := &stubLimiter{allow: func(string) (bool, error) {
			asked++

			// The bucket refills between attempts, which is what the retry
			// policy's backoff is really waiting for.
			return asked > 2, nil
		}}

		client := newClient(t,
			WithTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, "fine"), nil
			})),
			WithRateLimit(limiter),
			WithRetryPolicy(&immediatePolicy{attempts: 5}),
		)

		resp, err := get(t.Context(), client, "http://example.com/thing")
		must.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		test.EqOp(t, http.StatusOK, resp.StatusCode)
		test.EqOp(t, 3, asked)
	})

	T.Run("the breaker judges the outcome after retrying", func(t *testing.T) {
		t.Parallel()

		breaker := closedBreaker()

		var calls int
		client := newClient(t,
			WithTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				calls++
				if calls < 3 {
					return response(http.StatusBadGateway, "down"), nil
				}

				return response(http.StatusOK, "fine"), nil
			})),
			WithCircuitBreaker(breaker),
			WithRetryPolicy(&immediatePolicy{attempts: 5}),
		)

		resp, err := get(t.Context(), client, "http://example.com/thing")
		must.NoError(t, err)
		t.Cleanup(func() { _ = resp.Body.Close() })

		// Two transient 502s that a retry absorbed say nothing about the host's
		// health, so the breaker sees one success rather than two failures.
		test.EqOp(t, 3, calls)
		test.SliceLen(t, 1, breaker.SucceededCalls())
		test.SliceLen(t, 0, breaker.FailedCalls())
	})

	T.Run("a locally refused request is not held against the host", func(t *testing.T) {
		t.Parallel()

		breaker := closedBreaker()
		limiter := &stubLimiter{allow: func(string) (bool, error) { return false, nil }}

		client := newClient(t,
			WithTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
				t.Error("the request should never have reached the wire")

				return nil, nil
			})),
			WithCircuitBreaker(breaker),
			WithRateLimit(limiter),
		)

		_, err := get(t.Context(), client, "http://example.com/thing")
		must.Error(t, err)
		test.ErrorIs(t, err, ratelimiting.ErrRateLimited)

		// The breaker sees the error, but the wire was never touched: this says
		// something about the local budget and nothing about the host. Counting
		// it would let ordinary throttling trip — and keep tripped — a circuit
		// against a dependency that is perfectly well.
		test.SliceLen(t, 0, breaker.FailedCalls())
		test.SliceLen(t, 0, breaker.SucceededCalls())
	})
}
