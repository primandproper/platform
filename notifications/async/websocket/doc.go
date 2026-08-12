/*
Package websocket is a WebSocket-backed AsyncNotifier that holds its client
connections in process memory.

# This provider is correct at one replica

Connections live in this process, so Publish reaches only the clients connected
to this instance. Run two replicas behind a load balancer and a client connected
to replica A never sees an event published on replica B. It fails as a missing
notification rather than an error: nothing here detects the second instance, and
nothing reports the events it did not receive.

That is a constraint on the deployment, not a tuning knob. A service that scales
out wants the ably or pusher provider instead, where a hosted broker holds the
connections and every replica publishes through it.

# Why there is no messagequeue backplane

Fanning these events out over messagequeue was considered and rejected.

Such a backplane has to carry every event, including the ones bound for clients
on the replica that published: delivering locally *and* over the queue would
deliver twice to anyone connected here. So the broker's delivery guarantee
becomes this provider's guarantee. Redis pub/sub is the only broker anyone would
stand up for notification fanout, and it is at-most-once (see messagequeue's
Consumer docs) — which would make the single-replica case, the one that works
correctly today, less reliable than it is now. The multi-replica case it would
fix already has two working answers above.
*/
package websocket
