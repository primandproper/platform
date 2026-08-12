/*
Package grpc builds the gRPC server this module's services are served from: the
listener, the interceptor chain, TLS, health, and a shutdown that drains before
it flushes.

An application supplies its RegistrationFuncs and whatever interceptors it wants
and gets back a *Server with tracing and request logging already in the chain.
The platform's own interceptors are chained first, so an application interceptor
runs inside them and its work is inside the span.

# TLS is on only when both files are named

The server enables TLS when a certificate and its key are both configured, and
serves plaintext otherwise. That reading is why the config validates the two
together and refuses one without the other: naming a certificate and forgetting
the key is a configuration that looks like TLS and serves cleartext, and startup
is the only place that is cheap to notice.

When TLS is on it is TLS 1.2 or better with a fixed set of ECDHE cipher suites
and curves, and no client certificate is requested — this is server
authentication, not mutual TLS. A deployment needing mTLS needs its own
credentials, not an option here.

A port of zero is not an error: it asks the OS for an ephemeral one, which is
what a test wants.

# Health and reflection are opt-in

WithHealthRegistry registers grpc_health_v1 backed by a healthcheck.Registry —
the same registry the HTTP server answers /readyz from, so both transports report
from one set of checkers rather than two that can disagree. Passing it alongside
an application's own grpc_health_v1 registration panics, because gRPC rejects a
service registered twice.

Reflection is off by default. It enumerates every method and message the server
exposes to anyone who can reach the port, which is a convenience in development
and an inventory of the attack surface in production.

# Shutdown

Shutdown drains in-flight RPCs until ctx is done, then stops hard and reports
ctx's error, so a caller can tell a clean drain from a forced one. It flushes the
tracer provider afterwards — spans from RPCs that finish during draining would be
lost by flushing first — but does not shut the provider down, since the provider
belongs to the process rather than to this server.
*/
package grpc
