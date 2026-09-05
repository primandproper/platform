/*
Package grpc enforces authorization on gRPC methods.

An Enforcer pairs a frozen Requirements table with a
authorization.GrantsExtractor and produces both interceptors:

	reqs, err := authzgrpc.NewRequirements().
		RequireAll(mealplanning.MethodPermissions()).
		RequireAll(identity.MethodPermissions()).
		Public(healthpb.Health_Check_FullMethodName).
		Build()
	// ...

	enforcer, err := authzgrpc.NewEnforcer(reqs, extractGrants,
		authzgrpc.WithLogger(logger),
		authzgrpc.WithMetricsProvider(metricsProvider),
	)
	// ...

	server, err := grpcserver.NewGRPCServer(cfg,
		[]grpc.UnaryServerInterceptor{authInterceptor, enforcer.UnaryServerInterceptor()},
		[]grpc.StreamServerInterceptor{authStreamInterceptor, enforcer.StreamServerInterceptor()},
		registrations,
		grpcserver.WithLogger(logger), grpcserver.WithTracerProvider(tracerProvider),
	)

# One decision, two interceptors

Both interceptors are three lines around a single unexported check. That is the
main thing this package is for: hand-written unary and stream interceptors
enforcing "the same" rule drift apart, and they drift silently, because nothing
compares them. Here there is one rule and one place to change it, and the tests
drive the same table through both entry points.

# Ordering

Install this after authentication and inside the error-encoding interceptor:

  - Authentication must run first, or the extractor finds nothing and every
    call is denied. That case is counted separately
    (authorization_grpc_missing_grants) and logged at error level, because it is
    a wiring bug rather than an overreaching caller.
  - errors/grpc's encoding interceptor, when used, should wrap this one so the
    denial's error chain reaches the client and errors.Is works there too. The
    denial carries its own GRPCStatus, so the wire code is correct either way.

# Fail closed

A method absent from the table is denied. Public is how a method opts out, so
"needs no authorization" is a declaration rather than an omission, and
forgetting to register a method produces a denial rather than an opening.

Build refuses a method required with zero permissions. Vacuous truth means an
empty requirement would authorize everyone while reading like a restriction,
and that gap is exactly where an authorization hole hides. Build also refuses a
method declared twice, which is what happens when two service packages
contribute overlapping tables and one silently wins.

Requirements is immutable once built, so neither interceptor takes a lock. A
mutable table guarded by a mutex costs an acquisition on every RPC to protect a
map that is never written after startup.

# Rollout

WithAuditOnly evaluates and records every decision but denies nothing. Deploy
with it, watch authorization_grpc_denials and
authorization_grpc_undeclared_methods settle to zero, then remove it. Without
that step, enabling enforcement over a large hand-written table is a bet that
every entry is right on the first try.

# Watching it

Every instrument carries a method attribute, because one Enforcer serves every
method and a single mis-declared one is invisible in the total:
authorization_grpc_checks, authorization_grpc_denials,
authorization_grpc_undeclared_methods, and authorization_grpc_missing_grants.

The transport is in the name, so these stay distinguishable from the HTTP
middleware's authorization_http_* counters when a service installs both — an
un-suffixed name would read as a service-wide total that it is not.

Alert on undeclared_methods and missing_grants — both mean the wiring is wrong,
not that a caller misbehaved.

Decisions are attached to the ambient RPC span rather than a child span. An
authorization check is a few map lookups; a span per RPC to describe it would
double the trace volume of every service that installs this.
*/
package grpc

//platform:transport middleware: the same, as interceptors
