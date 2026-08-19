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

# Message sizes are bounded in both directions

grpc-go bounds a received message at 4 MiB and a sent one at math.MaxInt32,
which is no bound at all. That pairing is worse than it looks: a server on those
defaults will marshal and send a response no default-configured client can read,
and the ResourceExhausted surfaces on the caller — under its own 4 MiB receive
default, in a process the service owner may not operate, with nothing in the
server's logs or traces to say a response was ever too large.

This package bounds both directions at DefaultMaxMessageSize, so an oversized
response fails on the server, attributable to the handler that produced it.
Raising the bound is Config.MaxReceiveMessageSize / Config.MaxSendMessageSize —
deployment-time, because the right number depends on payloads rather than on
code — or WithMaxReceiveMessageSize / WithMaxSendMessageSize, which win over the
config the way caller options do everywhere else here. Zero from either source
takes the default; UnboundedMessageSize restores grpc-go's send behavior. The
bound is per message, so a stream is bounded once per message rather than once
per RPC.

A denormalized read model is the usual reason to raise the send bound. A page of
records that each embed their related records is larger than it reads: 250 rows
of a few dozen fields apiece clears 4 MiB without anything about the query
looking unusual, and a page-size ceiling and a message-size ceiling that were
chosen independently will disagree.

Raising the server's send bound is only half of it. The bound that actually
breaks a consumer is the client's receive bound, which this module does not
build and cannot set: a caller dials with
grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(n)) to match. A server
raised alone just moves the ResourceExhausted back to where it was hardest to
attribute.

# Shutdown

Shutdown drains in-flight RPCs until ctx is done, then stops hard and reports
ctx's error, so a caller can tell a clean drain from a forced one. It flushes the
tracer provider afterwards — spans from RPCs that finish during draining would be
lost by flushing first — but does not shut the provider down, since the provider
belongs to the process rather than to this server.
*/
package grpc
