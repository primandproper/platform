package httpclient_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	circuitbreakingcfg "github.com/primandproper/platform-go/v10/circuitbreaking/config"
	"github.com/primandproper/platform-go/v10/httpclient"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/ratelimiting"
	retrycfg "github.com/primandproper/platform-go/v10/retry/config"
)

// A provider integration composes its resilience once, at construction, instead
// of at every call site.
func ExampleNewHTTPClient_resilience() {
	ctx := context.Background()

	policy := retrycfg.NewExponentialBackoffPolicy(retrycfg.Config{
		MaxAttempts:  4,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     2 * time.Second,
		UseJitter:    true,
	})

	breaker, err := circuitbreakingcfg.NewCircuitBreaker(ctx, &circuitbreakingcfg.Config{
		Name:                   "payments",
		ErrorRate:              50,
		MinimumSampleThreshold: 20,
	})
	if err != nil {
		panic(err)
	}

	// The provider documents 10 requests per second; the burst absorbs the
	// bunching that a retrying client produces.
	limiter, err := ratelimiting.NewInMemoryRateLimiter(10, 20)
	if err != nil {
		panic(err)
	}

	// Whatever the service already built. Absent, every resilience layer below
	// resolves to its noop and the client records nowhere.
	pillars := &observability.Pillars{}

	client, err := httpclient.NewHTTPClient(
		httpclient.WithTimeout(30*time.Second), // room for the whole retry loop, not one attempt
		httpclient.WithTracing(true),
		httpclient.WithRetryPolicy(policy),
		httpclient.WithCircuitBreaker(breaker),
		httpclient.WithRateLimit(limiter),
		httpclient.WithPillars(pillars),
	)
	if err != nil {
		panic(err)
	}

	// Outermost to innermost the client is now: observability, breaker, retry,
	// rate limit, tracing, transport. Every package that builds its client
	// through httpclient gets the same arrangement without writing any of it.
	fmt.Println(client.Timeout)

	// Output: 30s
}

// A service whose status codes do not mean what the registry says they mean is
// the normal case, not the exceptional one. Both classification decisions are
// overridable, and both compose with the default rather than replacing it.
func ExampleWithOutcomeClassifier() {
	// This provider reports its own overload as 400 with a header, and answers
	// 503 for tenants that are merely out of quota. Taking either at face value
	// would trip a circuit against a host that is working perfectly well.
	classifier := func(resp *http.Response, err error) httpclient.Outcome {
		if resp != nil {
			switch {
			case resp.StatusCode == http.StatusBadRequest && resp.Header.Get("X-Overloaded") != "":
				return httpclient.OutcomeFailure
			case resp.StatusCode == http.StatusServiceUnavailable && resp.Header.Get("X-Quota-Exceeded") != "":
				return httpclient.OutcomeIgnored
			}
		}

		return httpclient.DefaultOutcome(resp, err)
	}

	overloaded := &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"X-Overloaded": {"1"}}}
	outOfQuota := &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"X-Quota-Exceeded": {"1"}}}
	genuine := &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}}

	fmt.Println(httpclient.DefaultOutcome(overloaded, nil), classifier(overloaded, nil))
	fmt.Println(httpclient.DefaultOutcome(outOfQuota, nil), classifier(outOfQuota, nil))
	fmt.Println(httpclient.DefaultOutcome(genuine, nil), classifier(genuine, nil))

	// Output:
	// success failure
	// failure ignored
	// failure failure
}
