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

# FIFO queues

A publish carries a body and a queue URL, plus whichever of the two FIFO fields
the caller asked for: messagequeue.WithOrderingKey becomes MessageGroupId and
messagequeue.WithDeduplicationKey becomes MessageDeduplicationId. Neither is
sent when its option is absent — the SDK omits a nil field and sends a present,
empty one — so a caller that names neither publishes exactly what this package
published before, which is what a standard queue wants.

Publishing to a FIFO queue needs both fields resolved, and only one of them can
be resolved here:

  - MessageGroupId is required on every message; SendMessage fails without it.
    messagequeue.WithOrderingKey supplies it. Messages sharing a group are
    delivered one at a time in order; different groups proceed in parallel.
  - MessageDeduplicationId is required too, unless the queue itself has
    ContentBasedDeduplication enabled — in which case SQS hashes the message
    body to derive one. With neither the explicit ID nor the queue setting,
    SendMessage fails. Whether the queue has it is a property of the queue,
    configured in AWS and not visible from here, so this package cannot
    supply the ID on a caller's behalf or warn that it will be needed: a
    caller publishing to a FIFO queue without content-based deduplication
    must pass messagequeue.WithDeduplicationKey.

Deduplication covers a five-minute window and applies per group. A standard
queue ignores MessageDeduplicationId and reads MessageGroupId as a fair-queue
tenant tag rather than an ordering constraint, so passing an ordering key to one
is harmless but buys no ordering.

None of this makes a standard queue ordered: without a FIFO queue behind the
URL, the semantics are still no ordering guarantee and duplicates possible
independent of handler failures.

# Lifecycle

The SQS client is a stateless HTTP client, so both providers' Close and the
publisher's Ping are no-ops. A message that fails to send is reported to the
caller; nothing here retries beyond what the AWS SDK does internally.

One consumer per queue per provider: a second NewConsumer for the same queue URL
returns messagequeue.ErrConsumerAlreadyRegistered rather than handing back the
first caller's consumer wired to the first caller's handler.
*/
package sqs
