/*
Package httpclient constructs HTTP clients with optional OpenTelemetry tracing
instrumentation, resilience middleware, and response caching.

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

Retry, circuit breaking, rate limiting, and response caching are
http.RoundTripper middlewares, composed at construction rather than at every
call site:

	client := httpclient.NewHTTPClient(
		append(cfg.Options(),
			httpclient.WithRetryPolicy(policy),
			httpclient.WithCircuitBreaker(breaker),
			httpclient.WithRateLimit(limiter),
			httpclient.WithHTTPCache(store),
		)...,
	)

Each is off unless named. A client built without them behaves exactly as it did
before they existed, and the collaborators come from the packages that own them
— retry.Policy, circuitbreaking.CircuitBreaker, ratelimiting.RateLimiter,
cache.Cache — so this package configures none of them and picks no defaults on
their behalf.

They are also not resolved from the injector. RegisterHTTPClient takes them as
options like everything else, because a RateLimiter or a CircuitBreaker in a
container is far more often the one guarding the service's own inbound API, and
silently repurposing it to throttle outbound calls would be a surprise nobody
asked for. The same holds for a cache, doubly so: a registered cache.Cache is
the service's own, and quietly filling it with third-party HTTP responses would
evict what it was built to hold.

# Response caching

The resilience transports protect an origin from failure, not from repetition.
WithHTTPCache adds the fourth middleware, over any cache.Cache — memory for a
per-process cache, redis for a fleet-wide one:

	client, err := httpclient.NewHTTPClient(
		httpclient.WithHTTPCache(store, httpclient.WithCacheTTL(5*time.Minute)),
	)

The policy is RFC 9111 read narrowly. GET and HEAD only; Cache-Control and
Expires decide freshness; ETag and Last-Modified drive revalidation, and a 304
refreshes the stored entry rather than replacing it. Freshness is judged against
a clock.Clock, so expiry is assertable in a test rather than slept through.

The explicit TTL is the reason most callers want this at all. JWKS documents,
.well-known metadata, and catalog endpoints are routinely served with no
freshness headers, and the hand-rolled TTL map that grows in front of them has
no revalidation, no Vary, and no size bound. WithCacheTTL is consulted last, so
naming one cannot make this client hold a response longer than the origin
permitted — it only fills the silence.

Two bounds are worth knowing. Bodies above WithMaxCacheableBody are returned in
full and not stored, so one large document cannot evict everything else. And one
variant per URL is retained: an entry records the request-header values named by
the response's Vary, and a request whose values differ is a miss rather than a
wrong answer.

Retention and freshness are separate knobs on purpose. The cache's own default
expiry decides how long an entry is kept; the headers above decide when it must
be revalidated. Keeping an entry past its freshness is what makes a 304
possible, so a store that expires entries the moment they go stale gives up the
cheaper half of this.

# What is never cached

A response to a request bearing Authorization, unless WithCacheAuthorized says
otherwise — and even then the credential becomes part of the cache key, so two
callers holding different tokens get different entries rather than each other's.
A shared Redis serving one tenant's response to another is the failure this is
designed against, not a corner case.

Also never stored: responses marked no-store or private, responses setting
cookies, responses that Vary: *, and any request already running its own
conditional exchange — an If-None-Match or a Range the caller set is a
precondition this transport has no business answering from a stored copy.

# Request signing

WithRequestSigning stamps an HMAC signature over every outgoing request body, so
a first-party callee can prove the call came from a holder of the shared key:

	client, err := httpclient.NewHTTPClient(
		httpclient.WithRequestSigning(signer),
		httpclient.WithRetryPolicy(policy),
	)

The signer comes from cryptography/requestsigning, whose keys are resolved
through secrets rather than captured at construction, so a rotation reaches the
wire without a restart. The inbound counterpart is requestsigning/http's
middleware, over the same scheme — one configuration governs both directions.

Every signed request's body is buffered whole, because a MAC over it cannot be
computed any other way. A client that streams large uploads should not sign them.

# The nesting is fixed

Outermost to innermost: observability, response cache, circuit breaker, retry,
rate limit, request signing, tracing, base transport. Option order does not
change it, because only one arrangement of these layers is right and a caller
who got it wrong would hold a client that looks protected and is not.

The cache is outermost because a hit is not a request. It reaches no wire, so it
must not report an outcome to a circuit breaker or spend a token from a budget
that counts requests the origin actually saw. A miss or a revalidation passes
through all three resilience layers exactly as an uncached request would.

The breaker is next so an open circuit rejects before the retry loop is entered
— failing fast once, rather than three times with backoff in between. It
therefore judges a host on final outcomes, after retrying has already absorbed
the transients.

The rate limiter is innermost so every attempt the retry loop makes spends a
token. A provider's documented budget counts requests on the wire, not the
caller's intentions, so a retry storm has to be charged against it. The other
arrangement — one token per logical call — would let a retrying client burst
straight past the budget it was configured to respect.

Signing is below even the limiter, for both of the reasons above read the other
way round. Below retry, because a signature carries a timestamp and the receiver
rejects a stale one: an attempt that reused the first attempt's signature would
arrive outside the tolerance after a long backoff, which is a failure that shows
up only in the requests already having a bad time. Below the limiter, because
signing costs a key resolution and an HMAC over the whole body, and a request
that never leaves should pay for neither.

Tracing sits below all of them, so each attempt is its own client span instead of
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
	cache_outcomes        how the response cache answered, by cache.outcome
	signing_failures      requests that could not be signed, and so never sent

The cache.outcome values partition every request that reaches the cache: hit
(answered without a wire request), revalidated (a 304 confirmed the stored
copy), miss (the origin answered in full), and uncacheable (a request the cache
took no part in). A cache that cannot be reached is counted as a miss and logged
at debug — the origin is still there, so an unreachable store should cost hit
rate and nothing else.

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
