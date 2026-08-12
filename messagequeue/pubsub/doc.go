/*
Package pubsub is a messagequeue publisher and consumer over Google Cloud
Pub/Sub.

Both providers take an already-built *pubsub.Client rather than credentials, so
authentication, the emulator, and any client-level tuning are the caller's to
arrange and this package inherits whatever they set up.

# Delivery

At-least-once. A handler that succeeds acks; one that returns an error nacks,
and what happens next belongs to the subscription rather than to this code —
its retry policy, its backoff, its dead-letter topic. Handlers must be
idempotent; see the idempotency package. The delivery attempt count, when
Pub/Sub supplies one, goes on the span.

Publish is synchronous despite the client batching underneath: it blocks until
the server has assigned a message ID, and returns that failure to the caller
rather than deferring it to a background flush. Nothing here retries beyond what
the client does internally.

# Names, and what must already exist

A short topic name is qualified to projects/{project}/topics/{name}; a name that
already starts with "projects/" is used as given. The publisher reaches the
topic directly and never calls GetTopic, which keeps the required IAM down to
pubsub.topics.publish — a service that only publishes does not need
pubsub.topics.get.

The consumer derives its subscription from the topic by substitution:
projects/{project}/subscriptions/{name}. The subscription must already exist and
must be named after its topic. Nothing here creates one, and a mismatch surfaces
at Consume time as a GetSubscription failure on the error channel rather than at
construction. Topic and subscription are provisioned by whatever manages the
project's infrastructure.

# Lifecycle

Ping is a no-op — Pub/Sub is a managed service and there is no endpoint worth
probing. Close on either provider closes the shared client, which is the one
resource either holds.

The consumer provider caches by topic and returns the cached consumer for a
repeat NewConsumer, rather than the ErrConsumerAlreadyRegistered its kafka,
redis, and sqs siblings return.
*/
package pubsub
