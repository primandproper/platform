/*
Package redis is a messagequeue publisher and consumer over Redis pub/sub.

This is pub/sub, not Redis Streams, and the difference is the whole of what
choosing it commits a caller to.

# At-most-once, with no way to notice

Nothing is persisted. A message published while no consumer is subscribed is
gone — Redis does not buffer for a subscriber that has not arrived yet, and
there is no backlog to read on connect. A handler that returns an error does not
see the message again; the error is reported on the consumer's error channel and
the loop moves to the next message. There is no acknowledgement, no redelivery,
and no dead-letter path, because pub/sub has none to expose.

Work that must not be lost belongs on kafka, pubsub, or sqs, all three of which
are at-least-once. What redis is for is fanout where a missed message is
tolerable and a broker nobody has to operate is worth more than the guarantee:
cache invalidation, presence, live counters.

The one loss this package does close is the subscription race. NewConsumer
blocks for a round trip until Redis confirms the SUBSCRIBE has been registered
server-side, so a publisher racing a starting consumer cannot slip a message
into the window between "we asked to subscribe" and "the server agreed".

# Fanout, not work distribution

Every subscriber on a channel receives every message. Two replicas of a service
consuming the same topic both run the handler for each message, which is the
opposite of what a consumer group does. There is no partitioning and no
ordering guarantee beyond what a single Redis connection happens to deliver.

# Lifecycle

Both providers validate their config at construction and build the client there,
so an address list Redis cannot use is a startup error rather than a nil client
that panics on first publish. A publisher's Stop is a no-op — the client is
shared across every topic, and the provider's Close is what releases it.
Consumers close their own subscriptions when their Consume loop exits.

One consumer per topic per provider: a second NewConsumer for the same topic
returns messagequeue.ErrConsumerAlreadyRegistered rather than handing back the
first caller's consumer wired to the first caller's handler.
*/
package redis
