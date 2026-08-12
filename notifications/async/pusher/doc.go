/*
Package pusher is a Pusher-backed AsyncNotifier.

It is one of the two fleet-safe providers in this family. Client connections are
held by Pusher rather than by this process, so a Publish from any replica
reaches every subscriber on the channel, and scaling out changes nothing about
delivery — which is exactly what the self-hosted sse and websocket providers
cannot do. See notifications/async for the topology argument in full.

Choosing it commits a deployment to a Pusher Channels app and to four values,
all required: the app ID, key, secret, and cluster. Config.Secure selects HTTPS
for the API calls this package makes.

# Publish only

The Notifier is a stateless HTTP client, which is why Close is a no-op and why
this type does not implement async.ConnectionAcceptor: there is no inbound
connection for this service to accept. Subscribers reach Pusher on their own.

The only vendor call wrapped here is the channel trigger. Pusher's
authentication endpoints for private and presence channels are not exposed, so a
deployment that needs subscriber authorization implements that route itself
against the Pusher SDK.

# The context does not reach the wire

Publish accepts a context and uses it for the span, but the SDK's trigger call
takes none: cancelling the context or letting its deadline lapse does not abort
a publish in flight. The ably sibling passes its context through to the request.
What bounds a publish here is the SDK client's own transport behavior.

The event's data is passed through as the raw JSON it already is.
*/
package pusher
