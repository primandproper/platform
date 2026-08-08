/*
Package httpclient constructs HTTP clients with optional OpenTelemetry tracing
instrumentation and resilience middleware.

Clients are built with functional options:

	client := httpclient.NewHTTPClient(
		httpclient.WithTimeout(5*time.Second),
		httpclient.WithTracing(true),
	)

Options are applied in order, so a later one overrides an earlier one. An
environment-loaded Config expresses itself as Options via Config.Options, so a
config-driven client is built the same way, and individual settings can still be
overridden after it:

	client := httpclient.NewHTTPClient(append(cfg.Options(), httpclient.WithTracing(true))...)

# Resilience

Retry, circuit breaking, and rate limiting are http.RoundTripper middlewares,
composed at construction rather than at every call site:

	client := httpclient.NewHTTPClient(
		append(cfg.Options(),
			httpclient.WithRetryPolicy(policy),
			httpclient.WithCircuitBreaker(breaker),
			httpclient.WithRateLimit(limiter),
		)...,
	)

Each is off unless named. A client built without them behaves exactly as it did
before they existed, and the collaborators come from the packages that own them
— retry.Policy, circuitbreaking.CircuitBreaker, ratelimiting.RateLimiter — so
this package configures none of them and picks no defaults on their behalf.

They are also not resolved from the injector. RegisterHTTPClient takes them as
options like everything else, because a RateLimiter or a CircuitBreaker in a
container is far more often the one guarding the service's own inbound API, and
silently repurposing it to throttle outbound calls would be a surprise nobody
asked for.

# The nesting is fixed

Outermost to innermost: circuit breaker, retry, rate limit, tracing, base
transport. Option order does not change it, because only one arrangement of the
three is right and a caller who got it wrong would hold a client that looks
protected and is not.

The breaker is outermost so an open circuit rejects before the retry loop is
entered — failing fast once, rather than three times with backoff in between.
It therefore judges a host on final outcomes, after retrying has already
absorbed the transients.

The rate limiter is innermost so every attempt the retry loop makes spends a
token. A provider's documented budget counts requests on the wire, not the
caller's intentions, so a retry storm has to be charged against it. The other
arrangement — one token per logical call — would let a retrying client burst
straight past the budget it was configured to respect.

Tracing sits below all three, so each attempt is its own client span instead of
one span spread over a loop.

# What retrying will and will not do

Only idempotent methods are retried, and only when the body can be replayed.
Both are properties a RoundTripper can check but not create: it cannot tell a
request that never arrived from a response that never came back, so it will not
repeat a POST on a guess. WithRetryMethods opts POST in for callers that pair it
with idempotency/http, whose transport sends one key across every attempt so the
server can recognize the repeat.

5xx, 408, and 429 are retried. Every other 4xx is reported to the policy as
retry.Unretryable, so the loop stops on the first one instead of spending its
attempts re-asking a question the server has already answered. Retry-After is
honored, capped by WithMaxRetryAfter; a server asking for longer than the cap
gets its response handed back rather than a retry that ignores what it asked for.

When attempts run out the caller gets the last response, not an error. A 503
that survived three tries is still the server's answer, and code that reads the
status does not need a second way to find it.

One thing to set deliberately: http.Client.Timeout bounds the whole loop,
retries and backoff included, because it becomes the request context's deadline
before the transport ever runs. A client that retries wants WithTimeout raised
to cover the attempts it is being asked to make.
*/
package httpclient
