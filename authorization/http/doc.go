/*
Package http enforces authorization on HTTP routes.

Requirements are declared where the route is registered:

	authz, err := authzhttp.NewEnforcer(extractGrants,
		authzhttp.WithLogger(logger),
		authzhttp.WithMetricsProvider(metricsProvider),
	)
	// ...

	routing.Get(router, "/recipes/{id}", readRecipe,
		routing.WithMiddleware(authz.Require(ReadRecipesPermission)))

# Why not a central table

The gRPC half of this package keys a table by grpc.UnaryServerInfo.FullMethod,
which is known before dispatch, and denies anything absent from it. The
equivalent here would key by route pattern — and a route pattern is not known
until the mux has matched. Global middleware installed via routing.Backend.Use
runs before matching, where chi's RoutePattern is still empty.

Making it work would mean either re-implementing path matching inside
platform-go, producing a second router that must agree with the real one, or
adding a per-backend hook to routing.Backend for every supported mux. Both cost
more than they buy for a seam whose requirement is one call at the registration
site — which also puts the requirement next to the handler it guards, where a
reader is most likely to notice its absence.

# The consequence, stated plainly

HTTP cannot fail closed on an undeclared route the way gRPC does. A route
registered without Require middleware is unguarded, and nothing here can detect
that.

What this package does guarantee is that a route which *is* guarded cannot be
reached without grants: Require denies when the extractor returns false, and
denies rather than vacuously allowing when handed an empty permission list.

Cover the rest with a test over your own registrations — assert every registered
route either carries authorization middleware or appears on an explicit
public-routes list.

That test is the answer and not a stopgap. A registration option inside routing
would see every route, but it annotates the OpenAPI operation and never sees the
request, so it could record that a route is guarded without being able to check
that it is — a route documented as protected while it serves anyone. Something
has to read the registrations and refuse; a test does, and it can read yours.

# Denials

The default response is the platform envelope: errors/http code E110 at HTTP
403, with the trace ID in details, identical to what the router emits for a
handler that returned ErrPermissionDenied. A service with its own envelope
replaces it with WithDenyHandler.

The body says "permission denied" and nothing else. Which permission was missing
goes to the span and the log; putting it in the response would tell an
unauthorized caller what to go looking for.

# Watching it

authorization_http_checks, authorization_http_denials, and
authorization_http_missing_grants. They are unlabeled totals.

The route is deliberately not a metric dimension. This middleware runs after
routing has matched the request but has no portable way to recover the pattern
that matched it — routing.Router exposes none — so the only label available is
the raw URL path, and a route with an identifier in it would produce one time
series per identifier.

The path is on the span instead, under authorization.method, where it costs one
attribute on one trace rather than a series that never stops growing. To
investigate a specific route, use the spans or the denial logs, which carry the
full request; use these counters for the rate and for alerting.
*/
package http

//platform:transport middleware: a route's declared requirement, checked before it runs
