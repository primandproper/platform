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
public-routes list. The durable fix is an option inside routing, which sees
every registration and can refuse to start; that belongs there rather than here.

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
authorization_http_missing_grants, each carrying the request path.

Note that the path is the raw URL path rather than the route pattern — for the
same reason there is no central table — so a route with an identifier in it
produces one series per identifier. Aggregate accordingly, and prefer the
denial logs, which carry the full request, when investigating a specific route.
*/
package http
