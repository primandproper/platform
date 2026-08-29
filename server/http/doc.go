/*
Package http is the module's HTTP server: a listener, a
[github.com/primandproper/platform-go/v13/routing] router, a graceful shutdown,
and the handful of endpoints that belong to the process rather than to the API
it serves.

	server, err := httpserver.NewHTTPServer(ctx, cfg, router,
		httpserver.WithServiceName("api"),
		httpserver.WithHealthRegistry(registry),
		httpserver.WithVersionEndpoint(),
		httpserver.WithLogger(logger),
		httpserver.WithTracerProvider(tracerProvider))
	if err != nil {
		return err
	}

	return server.Serve(ctx)

Serve blocks until Shutdown is called or ctx is done, and reports a bind failure
as an error rather than panicking through a hard-wired panicker: a library
cannot decide that a port already in use should take the host process down.
[github.com/primandproper/platform-go/v13/server/grpc] is the sibling, and the
options the two share are spelled the same way in both.

# What the process serves, as opposed to what the service serves

Three things here are not part of anybody's API, and all three mount on the
router's backend rather than through routing's typed registration — so they get
the router's middleware and stay out of the OpenAPI document a generated client
is built from:

	/healthz   LivenessHandler, a constant body, allocating nothing per probe
	/readyz    ReadinessHandler, the health registry's per-component verdict
	/version   VersionHandler, whatever -ldflags stamped into the binary

The probes are opted into with WithHealthRegistry and the version endpoint with
WithVersionEndpoint, rather than always mounted, because a service that already
answers at those paths would otherwise find them registered twice and most muxes
answer that with a panic. Each handler is also exported on its own, so a
deployment that binds a separate operational listener gives the same answers
this server would have.

Version is separate from the probes because it carries a different decision, not
just a different answer: the commit a process is running is useful to an
operator and is also information about the deployment, so exposing it on a public
listener is said rather than inherited.

# Static files and Universal Links

RootLevelAssetsHandler serves root-level files out of a directory, refusing
subdirectories and path traversal, for the single-page app whose bundle ships
beside the binary. Register it last, as the catch-all.

AppleAppSiteAssociationHandler serves /.well-known/apple-app-site-association
from configuration — the document iOS fetches to associate a domain with an app.
NewHTTPServer mounts it when Config.AppleAppSiteAssociation is enabled, so
calling the handler directly is for serving the document from somewhere else. A
config that is empty or malformed yields a 404 rather than a document iOS would
reject, and a partially filled one fails validation at startup: paths scoped but
a TeamID that never made it out of the environment is the case that would
otherwise validate clean and serve nothing.

# Shutdown

Shutdown drains in-flight requests and flushes their spans. It does not shut the
tracer provider down — the provider was handed to this server, not built by it,
and closing its exporter would silence the gRPC sibling still draining and every
background loop whose Close runs after ingress stops. Whoever built the provider
shuts it down;
[github.com/primandproper/platform-go/v13/observability.Pillars.Shutdown] is that
owner, and service.Service runs it last.

# Where the handlers are not

This package serves the process's own endpoints and nothing of any consumer's
domain. Which components of this module ship an HTTP surface, and which ship a
store and stop there, is stated once in the module README under "Stores and
Transports".
*/
package http
