/*
Package http adapts ratelimiting to inbound HTTP.

It is a routing.Middleware that spends a token per request and answers 429 when
there is none — the inbound counterpart to httpclient.WithRateLimit, over the
same ratelimiting.RateLimiter, so one configured limiter governs both directions.

	limiter, err := ratelimitingcfg.NewRateLimiter(ctx, cfg.RateLimiting)
	if err != nil {
		return err
	}

	mw, err := ratelimitinghttp.NewMiddleware(limiter,
		ratelimitinghttp.FirstNonEmpty(
			keyByPrincipal,                              // yours
			ratelimitinghttp.KeyByHeader("X-API-Key"),
			ratelimitinghttp.KeyByRemoteAddr(),
		),
		ratelimitinghttp.WithMetricsProvider(pillars.Metrics),
	)
	if err != nil {
		return err
	}

	router.Use(mw)

It never reads the request body, so a global Router.Use costs upload routes
nothing — unlike the idempotency middleware, which is documented as per-route
for exactly that reason. Install it per route with routing.WithMiddleware when
one endpoint is far more expensive than the rest and deserves its own budget.

# Keying

The key is what "N per second per what" resolves to, and there is no default
because the wrong answer is worse than no limiter at all. Keying an
authenticated API on addresses pools a whole office behind one bucket; keying a
public one on a client-supplied header hands out a fresh bucket per request.

KeyByRemoteAddr is the only extractor that is safe with nothing in front of the
server. Behind a proxy, KeyByForwardedFor(n) reads the client address out of
X-Forwarded-For, counting n trusted appending hops from the right — a CDN in
front of a load balancer is two, not one. KeyByHeader hashes what it reads,
because a limiter key becomes a Redis key and an API key that lands in a
keyspace has been disclosed.

A KeyFunc that returns "" exempts the request. That is how a route says it is
counted somewhere else rather than twice.

# Retry-After

A refusal without a hint relocates load rather than shedding it: every refused
client picks its own interval, and clients that guess tend to guess alike. So
the middleware asks the limiter when to come back, via ratelimiting.RetryHinter
— the in-memory and Redis limiters both implement it — and falls back to
DefaultRetryAfter for the limiters that cannot answer. WithoutFallbackRetryAfter
sends nothing rather than a guess.

# When the limiter cannot answer

Redis unreachable, or a key extractor that failed, is a fault in the guard
rather than a verdict from it. By default those requests are let through and
counted in ratelimiting_http_errors: failing closed would turn one dependency's
bad minute into a total outage of the thing being guarded. WithFailClosed
inverts that for endpoints where admitting everyone is the worse failure.

# The wire shape

Refusals render through routing.DefaultErrorBody: the platform APIError
envelope with code E116, exactly what the Router produces for a handler that
returned ratelimiting.ErrRateLimited. A service that replaced that envelope
passes its own encoder to WithErrorEncoder — the same one it gave the Router —
so a 429 arrives in the shape its clients already parse.
*/
package http
