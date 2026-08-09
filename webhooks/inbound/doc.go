/*
Package inbound receives webhooks: it verifies the provider's signature over
the bytes as they arrived, publishes the delivery to a message queue, and acks.

It is the mirror of the parent webhooks package, which sends them.

# The two failures it exists to remove

Verification is the first. Every provider signs differently and every consumer
implements the scheme again, and being subtly wrong is silent: a comparison
that is not constant-time, a timestamp with no tolerance window, an HMAC over
the decoded body instead of the bytes as received. None of those fail a test
written against the happy path, and all of them are the whole security of the
endpoint. Verifier is the seam, and the schemes behind it are this package's.

Ack latency is the second. A handler that does its work inline couples the
provider's ack deadline to how long that work takes. Providers time out in the
tens of seconds and retry anything that is not 2xx, so on the afternoon the
database is slow the work completes, the ack misses the window, and the same
event arrives again and is processed a second time. Sustained failures get the
endpoint disabled outright — Stripe and GitHub both do this — at which point
every subsequent event is lost until somebody notices. A Receiver does one
bounded thing before it acks, which is publish.

# What it does not do

It does not parse the payload. Parsing is what couples a receiver to a
provider's schema and therefore to that provider's schema versioning, and the
consumer has to decode the body anyway in order to act on it. Delivery carries
the bytes.

It does not dedupe. A redelivery is expected — it is how a provider recovers
from a missed ack — but "already processed" is a statement about the
consumer's own effects, not about receipt, and only the consumer can make it.
Key an idempotency.Manager on the provider's event ID at the point the work
happens; see the idempotency package.

It does not store anything. An inbound receiver has no local row to keep atomic
with a downstream effect, which is the problem an outbox and an events table
solve. Its whole job is moving bytes from an HTTP request into a durable place:
durability is the broker's, retry is the provider's, and if the publish fails
the receiver simply does not ack. It holds nothing worth losing, and holding
nothing is what keeps it free of a database and of any one database's dialect.

The cost of that is real and worth stating. There is no local record to answer
"what did Stripe actually send us at 3am", and no replay by event ID — for
which the provider's own event log (Stripe's resend, GitHub's redelivery API)
is authoritative and a local copy would only ever be a cache. An archive of raw
deliveries composes on top as another consumer of the same topic; it is not a
thing the ack path should be waiting on.

# Poison messages

An event the consumer can never process needs somewhere to land, and once a
Receiver has returned 2xx the provider is done with it. That is dead-letter
behavior, it belongs to the broker, and it is configured there — an SQS
redrive policy, a Pub/Sub dead-letter topic — not mediated by this package or
by messagequeue, which exposes no such seam. A backend without one (Redis) has
no dead-letter story to configure, and a consumer running on it owns the
decision to drop or park an event it cannot handle. Choosing the broker chooses
the answer, which is why this package does not offer a second one.

# Using it

One Receiver per provider endpoint, holding one Publisher and one Verifier:

	verifier, err := inbound.NewStripeVerifier(cfg.StripeWebhookSecret)
	if err != nil {
		return err
	}

	receiver, err := inbound.NewReceiver(verifier, publisher,
		inbound.WithReceiverLogger(logger),
		inbound.WithReceiverTracerProvider(tracerProvider),
		inbound.WithReceiverMetricsProvider(metricsProvider),
	)
	if err != nil {
		return err
	}

	receiver.Mount(router, "/webhooks/stripe")

Mount registers a POST route through routing.Handle, which records no OpenAPI
operation. That is deliberate: the request body is opaque provider JSON whose
schema this package does not know and should not publish as if it did.

# Headers are not authenticated

Delivery.Headers carries what arrived, because a provider puts things there a
consumer needs — GitHub's X-GitHub-Delivery is the delivery ID and appears
nowhere else. They are not covered by any of these signatures, which sign the
body (and, for Stripe, a timestamp). A consumer must therefore treat header
values as untrusted for anything security-relevant, and read what matters from
the verified body. Credential headers are dropped rather than forwarded, and
WithForwardedHeaders narrows the set further.
*/
package inbound
