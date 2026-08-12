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

# Ordering

messagequeue.WithOrderingKey sets the Kafka message key, and the writer
partitions on a murmur2 hash of it, so all events for one account land on one
partition and arrive in the order they were published. That is the usual reason
to choose Kafka, and it is the reason the writer names its balancer: kafka-go's
default is RoundRobin, which ignores the key, so a key without a matching
balancer would be carried to the broker and change nothing.

The balancer is Murmur2 with Consistent left off, which is librdkafka's
"murmur2_random" and hashes keys the way the Java producer does — a topic this
package writes to partitions the same as one written by any other client. A
publish that names no ordering key sends a nil key and is placed on a partition
at random, so unkeyed traffic stays spread rather than piling onto whichever
partition the empty key hashes to.

Ordering survives to the consumer because Kafka assigns each partition to one
member of a consumer group, and this package's consumer processes the messages
it fetches one at a time. Two keys that hash to the same partition are ordered
against each other as well; that is a consequence of partitioning, not a
guarantee, and a key that needs its own sequence needs nothing more than being
its own key.

messagequeue.WithDeduplicationKey is accepted and ignored. Kafka's idempotent
producer deduplicates on its own producer/sequence numbers, not on a
caller-supplied key.

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
