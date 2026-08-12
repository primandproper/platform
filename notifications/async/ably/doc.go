/*
Package ably is an Ably-backed AsyncNotifier.

It is one of the two fleet-safe providers in this family. Client connections are
held by Ably rather than by this process, so a Publish from any replica reaches
every subscriber on the channel, and scaling out changes nothing about delivery
— which is exactly what the self-hosted sse and websocket providers cannot do.
See notifications/async for the topology argument in full.

Choosing it commits a deployment to an Ably account and one API key, and commits
its clients to connecting to Ably directly.

# Publish only

The Notifier speaks REST and holds no persistent connection, which is why Close
is a no-op and why this type does not implement async.ConnectionAcceptor: there
is no inbound connection for this service to accept. Subscribers reach Ably on
their own.

The consequence for a caller is that everything besides publishing is outside
this package. Token authentication for private channels, presence, and history
are Ably features this package does not wrap; a deployment that needs subscriber
authorization arranges it against Ably itself.

# Payloads are decoded before they are sent

An async.Event carries its data as raw JSON bytes. Handing those bytes to
ably-go directly makes it treat them as binary and base64-encode them, so
subscribers receive an opaque blob where every other backend in this family
delivers JSON. Publish therefore unmarshals the payload into a Go value first,
and ably-go transmits it as a JSON object or array.

That has a visible edge: a payload that is not valid JSON fails at Publish with
a decode error rather than arriving mangled. An empty payload is sent as no
data.

Publish takes a context and passes it through to the request, so a caller's
deadline and cancellation apply — the pusher sibling's does not.
*/
package ably
