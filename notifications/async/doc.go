/*
Package async provides a channel-based async event delivery interface with
implementations for WebSocket, SSE, Pusher, and Ably.

# Two behavior classes

The providers differ in one way the AsyncNotifier interface does not express,
and it is the difference that matters most in production:

  - pusher and ably are fleet-safe. A hosted broker holds the client
    connections, so a Publish from any replica reaches every subscriber.
  - sse and websocket hold connections in this process's memory. A Publish on
    replica A reaches only the subscribers connected to replica A — and misses
    the rest silently, as absent notifications rather than as an error.

The self-hosted providers are therefore correct only at a single replica. That
constraint used to be written down nowhere, which is the failure this
documentation and the Topology declaration in the config subpackage exist to
prevent: a service that scales from one replica to two acquires a notification
bug with no error, no log line, and no failed request to trace.

# Declaring topology

A process cannot detect how many replicas of itself are running, so the
constraint cannot be enforced automatically — it has to be declared. The config
subpackage requires an explicit Topology for the self-hosted providers, and
refuses the combination of a self-hosted provider and a fleet. Choosing sse or
websocket therefore means choosing single-replica out loud, and wanting more
than one replica means choosing a hosted provider.
*/
package async
