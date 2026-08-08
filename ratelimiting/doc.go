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
