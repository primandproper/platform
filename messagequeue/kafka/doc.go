/*
Package kafka is a messagequeue publisher and consumer over Apache Kafka.

Choosing it commits a deployment to a Kafka cluster it operates or rents, to a
broker list, and to a consumer group ID — all three required. It is the only
provider in this family that delivers at-least-once with a durable, replayable
log behind it, and the only one where a handler failure stops the consumer.

# Delivery

At-least-once. The consumer fetches a message, runs the handler, and only then
commits the offset, so a crash between the two redelivers on restart. Handlers
must be idempotent; see the idempotency package.

A handler that returns an error halts the consume loop and returns, leaving the
offset uncommitted. That is deliberate and it is specific to this provider:
Kafka commits are cumulative by offset, so committing any later message would
advance the group past the failed one and lose it. The redis, pubsub, and sqs
consumers log the failure and continue with the next message; here, one bad
message stops consumption of that topic until the process restarts or the group
rebalances — at which point the same message is delivered again. A poison
message will therefore loop unless the handler eventually decides to accept it.

Publishes wait for acknowledgement from all in-sync replicas. The writer's batch
timeout is lowered to 10ms because Publish is synchronous and typically carries
one message, and the library's one-second default would otherwise put a
one-second floor under every publish.

Missing topics are created automatically on publish, which is convenient in
development and worth knowing about in production, where it means a typo in a
topic name produces a new topic rather than an error.

# No ordering key

Messages are written with a value and no key, so the writer's default balancer
places them across partitions and no two messages are guaranteed to be ordered
relative to each other. Per-entity ordering — all events for one account landing
in one partition in order — is the usual reason to choose Kafka, and it is not
available through this package as written.

# Lifecycle

Constructing either provider does not touch the network; a bad broker list
surfaces on the first publish or fetch. The publisher provider's Ping dials the
first broker. Each consumer owns its reader and closes it when Consume exits, so
the consumer provider's Close has nothing to do.

One consumer per topic per provider: a second NewConsumer for the same topic
returns messagequeue.ErrConsumerAlreadyRegistered. Handing back the cached one
would give this caller someone else's handler and, once a handler error has
stopped that loop, a consumer that is permanently dead.
*/
package kafka
