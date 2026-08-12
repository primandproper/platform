/*
Package sqs is a messagequeue publisher and consumer over Amazon SQS.

Credentials and region are not in Config. They come from the ambient AWS chain
— instance role, container role, environment, shared profile — resolved at
construction, so choosing SQS commits a deployment to whatever IAM identity the
process runs as. Config.QueueAddress only overrides the endpoint the SDK would
resolve, which is what points the package at localstack; leaving it empty is the
ordinary case.

# The topic is a queue URL

Everywhere else in messagequeue a topic is a short name. Here the string passed
to NewPublisher and NewConsumer is the queue's full URL, and the queue must
already exist — nothing here creates one.

# Delivery

At-least-once. A handler that succeeds deletes the message; one that returns an
error leaves it, so it reappears after the queue's visibility timeout. Retry
limits and dead-lettering are the queue's own redrive policy, configured in AWS
rather than here. Handlers must be idempotent; see the idempotency package.

The consumer long-polls: each receive waits up to 20 seconds for up to 10
messages, which paces the loop without spinning. A receive that fails returns
immediately, so those are backed off explicitly before retrying.

# Standard queues only

Publishes carry a body and a queue URL and nothing else — no MessageGroupId and
no MessageDeduplicationId. A FIFO queue requires a group ID on every message, so
this package cannot publish to one, and with it goes ordering and provider-side
deduplication. Standard-queue semantics are what is on offer: no ordering
guarantee, and duplicates possible independent of handler failures.

# Lifecycle

The SQS client is a stateless HTTP client, so both providers' Close and the
publisher's Ping are no-ops. A message that fails to send is reported to the
caller; nothing here retries beyond what the AWS SDK does internally.

One consumer per queue per provider: a second NewConsumer for the same queue URL
returns messagequeue.ErrConsumerAlreadyRegistered rather than handing back the
first caller's consumer wired to the first caller's handler.
*/
package sqs
