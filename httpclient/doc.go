/*
Package httpclient constructs HTTP clients with optional OpenTelemetry tracing
instrumentation and resilience middleware.

Clients are built with functional options:

	client, err := httpclient.NewHTTPClient(
		httpclient.WithTimeout(5*time.Second),
		httpclient.WithTracing(true),
	)

Options are applied in order, so a later one overrides an earlier one. An
environment-loaded Config expresses itself as Options via Config.Options, so a
config-driven client is built the same way, and individual settings can still be
overridden after it:

	client, err := httpclient.NewHTTPClient(append(cfg.Options(), httpclient.WithTracing(true))...)

The error reports only that the client could not be instrumented — the metrics
provider refused an instrument. Nothing else here can fail.

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

Outermost to innermost: observability, circuit breaker, retry, rate limit,
tracing, base transport. Option order does not change it, because only one
arrangement of the three resilience layers is right and a caller who got it
wrong would hold a client that looks protected and is not.

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

By default 5xx, 408, and 429 are retried, and every other 4xx is reported to the
policy as retry.Unretryable, so the loop stops on the first one instead of
spending its attempts re-asking a question the server has already answered.
Retry-After is honored, capped by WithMaxRetryAfter; a server asking for longer
than the cap gets its response handed back rather than a retry that ignores what
it asked for.

# Classification is a default, not a rule

Two decisions above are stated in terms of status codes, and status codes are
the part of HTTP that services agree on least. Both are overridable, and both
default to the registry's reading:

	WithRetryPolicy(policy, WithRetryClassifier(fn))     // is this worth another attempt?
	WithCircuitBreaker(breaker, WithOutcomeClassifier(fn)) // what did this say about the host?

They are separate questions and a single answer would serve neither. A 429 is
worth retrying but says nothing about whether the host is healthy; a 400 is
neither; a 503 is both. Delegate to DefaultRetryClassification and DefaultOutcome
for whatever a classifier does not have an opinion about, rather than restating
the rules it means to keep.

The outcome classifier is three-valued — success, failure, ignored — because a
request can fail without the host having done anything wrong. A request this
client's own limiter refused is the built-in case: it never reached the wire, so
counting it either way would be a lie, and counting it as a failure would let
ordinary throttling trip a circuit against a host that is perfectly well.

# Observability

The resilience layers report through the standard pillars, supplied with
WithLogger, WithTracerProvider, and WithMetricsProvider, or with WithPillars for
all three. Absent, each resolves to its noop and the client records nowhere.

Metrics, all prefixed httpclient_ and attributed by host and method — never by
URL, which is unbounded:

	retry_attempts        attempts beyond the first
	retries_exhausted     loops that retried and still gave up, by final status
	retry_after_seconds   Retry-After delays actually honored
	circuit_rejections    requests refused by an open circuit
	circuit_outcomes      how each completed request was classified
	rate_limited          requests the local limiter refused

Two log lines are worth knowing about. A request refused by an open circuit
is logged where it happens, because it produces no response, no attempt, and no
span of its own — without that line, a client that has stopped talking to a
dependency altogether looks exactly like one with nothing to say. And a loop
that exhausted its attempts is logged with the error it gave up on, because
RoundTrip deliberately discards that error in favor of the last response: the
right answer for the caller, and one that would otherwise make a request that
burned four attempts indistinguishable from one that succeeded immediately.

The outermost layer opens a span covering the logical request, which is what
gives the per-attempt spans below the retry loop a parent, and what puts a
breaker or limiter rejection into a trace at all. It follows WithTracerProvider
rather than WithTracing: the two describe different things, and there is no
reason to configure a tracer provider and want the resilience layers left out
of it.

When attempts run out the caller gets the last response, not an error. A 503
that survived three tries is still the server's answer, and code that reads the
status does not need a second way to find it.

One thing to set deliberately: http.Client.Timeout bounds the whole loop,
retries and backoff included, because it becomes the request context's deadline
before the transport ever runs. A client that retries wants WithTimeout raised
to cover the attempts it is being asked to make.
*/
package httpclient
