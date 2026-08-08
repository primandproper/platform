package httpclient_test

import (
	"context"
	"fmt"
	"time"

	circuitbreakingcfg "github.com/primandproper/platform-go/v9/circuitbreaking/config"
	"github.com/primandproper/platform-go/v9/httpclient"
	"github.com/primandproper/platform-go/v9/ratelimiting"
	retrycfg "github.com/primandproper/platform-go/v9/retry/config"
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

	client := httpclient.NewHTTPClient(
		httpclient.WithTimeout(30*time.Second), // room for the whole retry loop, not one attempt
		httpclient.WithTracing(true),
		httpclient.WithRetryPolicy(policy),
		httpclient.WithCircuitBreaker(breaker),
		httpclient.WithRateLimit(limiter),
	)

	// Outermost to innermost the client is now: breaker, retry, rate limit,
	// tracing, transport. Every package that builds its client through
	// httpclient gets the same arrangement without writing any of it.
	fmt.Println(client.Timeout)

	// Output: 30s
}
