/*
Package ratelimiting provides a per-key rate limiter interface using the token
bucket algorithm.

RateLimiter is deliberately narrow: Allow says yes or no for a key, and Close
releases whatever the implementation held. Implementations live in this package
(in-memory), ratelimiting/redis (shared, sliding window), and ratelimiting/noop.
Select one from configuration with ratelimiting/config.

# Refusals

Allow expresses a refusal as (false, nil), because the caller is usually
deciding what to do next rather than propagating a failure. ErrRateLimited
exists for the callers that have to hand a refusal back as an error instead —
an http.RoundTripper has nowhere else to put it. errors/http maps that sentinel
to 429 and errors/grpc to RESOURCE_EXHAUSTED, so a refusal crosses the wire as
itself rather than as whatever the fallback mapping produces. errors/http maps
back too: ErrorForCode turns the E116 in a response envelope into this same
sentinel, which is what lets a typed client hand its caller the value an
in-process caller would have gotten.

# Retry hints

RetryHinter is the optional half of the interface: an implementation that can
say when a refused key will next be allowed implements it, and callers ask
through RetryAfterFor rather than asserting for it themselves. It is separate
from RateLimiter because a limiter fronting a third party often cannot answer,
and an invented Retry-After is worse than none — clients obey it.

# What the in-memory limiter keeps

The in-memory limiter holds one token bucket per key, and the keys are whatever
the caller limits on — client addresses and principal IDs, for the two obvious
choices, neither of which has a bounded key space. So it reclaims them: a key
that has gone unconsulted for twice its window is dropped on the next pass of a
sweeper the constructor starts and Close stops.

The window is the time a bucket takes to refill a full burst at the steady rate,
which is the same quantity ratelimiting/redis turns into the length of its
sliding window and expires its keys against. It is derived rather than
configured because it is the point past which a bucket has nothing left to
remember: a key that returns after the TTL is handed a full burst, which is
exactly what the bucket it left behind would have refilled to. Eviction is
therefore invisible to callers, and Close is not optional — an unclosed limiter
keeps its sweeper, and itself, alive for the life of the process.

The TTL cannot cover one case: a flood of distinct keys inside a single window,
where nothing has been idle long enough to reclaim. That is what
DefaultMaxLimiters bounds, evicting the least recently seen — the buckets
closest to being refilled, and so the cheapest to forget. Unlike a TTL eviction
this one does forgive whatever the evicted keys still owed, which is why the two
are counted separately: a non-zero rate of capacity evictions says the bound is
being hit and some keys are getting their allowance back early. Raise it with
WithMaxLimiters.

# Guarding a service

The transport adapters are ratelimiting/http (a routing.Middleware answering
429, with Retry-After) and ratelimiting/grpc (a unary interceptor answering
RESOURCE_EXHAUSTED, with RetryInfo). Outbound, httpclient.WithRateLimit spends
from a limiter before each request. All three take the same RateLimiter, so one
configured limiter can govern a service's inbound and outbound traffic alike.

# Not quota

This package answers "too fast right now". "Too much this month" is metering:
the two have different remedies — wait versus buy more — and conflating them
tells a client to retry when it should stop.
*/
package ratelimiting
