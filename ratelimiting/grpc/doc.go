/*
Package grpc adapts ratelimiting to inbound gRPC.

It is a unary server interceptor that spends a token per RPC and answers
RESOURCE_EXHAUSTED when there is none — the gRPC counterpart to
ratelimiting/http's middleware, over the same ratelimiting.RateLimiter, so a
service on both transports can hold one limiter and have a caller's budget mean
the same thing on either.

	interceptor, err := ratelimitinggrpc.NewUnaryServerInterceptor(limiter,
		ratelimitinggrpc.FirstNonEmpty(
			keyByPrincipal,                                  // yours
			ratelimitinggrpc.KeyByMetadata("x-api-key"),
			ratelimitinggrpc.KeyByPeer(),
		),
		ratelimitinggrpc.WithMetricsProvider(pillars.Metrics),
	)
	if err != nil {
		return err
	}

	srv, err := servergrpc.NewServer(ctx, cfg,
		[]grpc.UnaryServerInterceptor{authInterceptor, interceptor},
		nil,
	)

Order matters: install it after authentication, so a key function reading a
principal has one to read.

# Keying

See KeyByPeer, KeyByMetadata, PerMethod, and FirstNonEmpty. There is no default
key, because the wrong one is worse than none — KeyByPeer behind a proxy pools
every caller into one bucket, and metadata the caller writes is a fresh bucket
per RPC on request.

PerMethod scopes a key to the RPC being called, so one expensive method cannot
spend the budget its neighbors need.

# RESOURCE_EXHAUSTED and RetryInfo

The refusal is a status error whose code comes from errors/grpc's mapping of
ratelimiting.ErrRateLimited, and which unwraps to that sentinel — so an
in-process caller and a remote one branch on the same value. Its message is a
constant, so a refused caller learns nothing about the limit it hit.

When the limiter can say when to come back — ratelimiting.RetryHinter, which the
in-memory and Redis limiters implement — the delay rides along as
google.rpc.RetryInfo, the canonical error model's Retry-After. Clients and
proxies already know to read it; without it they guess, and clients that guess
tend to guess alike, which turns shed load into a synchronized storm.

# What is not covered

Streams. A stream spends one token at open and then runs unbounded, which
measures the wrong thing badly enough to be worth leaving out rather than
shipping as a guard people trust. Rate-limit the messages inside the stream, or
limit stream opens with your own interceptor and be explicit that that is what
it counts.

Quota. "Too fast right now" is this package; "too much this month" is metering.
*/
package grpc

//platform:transport middleware: the same, as interceptors
