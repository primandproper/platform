/*
Package webhooks delivers outbound webhooks: signed, retried, ordered, and
replayable.

Its mirror is webhooks/inbound, which receives them: verify the provider's
signature, publish, ack.

Everything here is a guarantee rather than an opinion. What an event means, when
it fires, and what its payload contains are the application's; that a subscriber
can authenticate the payload, that a delivery is not lost when the process dies,
that a dead subscriber cannot starve a healthy one, and that resource.updated
cannot overtake resource.created are this package's.

# The two halves

Dispatcher is the write side. Dispatch takes the caller's transaction executor,
resolves who is subscribed, and writes one dispatch row per subscriber inside
that transaction:

	err := client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		if err := updateOrder(ctx, q, order); err != nil {
			return err
		}

		return dispatcher.Dispatch(ctx, q, &webhooks.Delivery{
			EventType:   "order.updated",
			OrderingKey: order.ID,
			Payload:     body,
		})
	})

The deliveries commit with the state change that caused them, or not at all.
There is no way to dispatch outside a transaction by accident: holding a
SQLQueryExecutor from WithTransaction means you are already in one. This is the
same seam outbox.Enqueue uses, for the same reason.

Worker is the delivery side. It claims due dispatches, signs and sends them,
records every attempt, and schedules retries. It runs in its own process or
goroutine and is started by Run, stopped by Close.

# Why dispatches are rows and not queue messages

The unit of retry is one endpoint's copy of one event, and no broker can express
that. A delivery that fanned out to five subscribers is not "failed" or
"delivered" — four may have accepted it on the first attempt while the fifth is
on its sixth retry. Retrying at the message level redelivers to the four that
already succeeded; tracking state at the message level cannot represent the
fifth's attempt count at all.

So per-endpoint state lives in a row: attempts, next_attempt, last_error, and a
terminal dead flag, per (delivery, endpoint). Retry is a scheduled timestamp
rather than a nack, which means it survives a worker restart, and "give up" is a
state a row is in rather than a message that stopped being redelivered.

# Signing

	X-Platform-Signature: v1,t=1753900000,s=<hex(HMAC-SHA256(secret, "v1." + t + "." + body))>

The scheme is cryptography/requestsigning, and this package is one of its
callers rather than its owner. Secret is requestsigning.Keyring, deliveries are
signed with requestsigning.Sign, and a subscriber verifies them with
requestsigning.Verify — the same functions guarding first-party
service-to-service calls, over the same wire format. Read that package for why
both the version and the timestamp are inside the signed material, and for what
the Current/Previous pair buys.

What is worth repeating here is the receiving end. Verification is where these
schemes are actually got wrong: subscribers compare with ==, skip the timestamp
check, or verify a re-serialized body rather than the received bytes. Point
subscribers at requestsigning.Verify rather than at the paragraph above — a
scheme described in prose is a scheme reimplemented, and reimplemented
verification is where the timing leak goes.

	body, err := io.ReadAll(req.Body)   // the exact bytes, before any decoding
	if err != nil {
		return err
	}

	err = requestsigning.Verify(secret, body, req.Header.Get(requestsigning.SignatureHeader))

A subscriber that is itself a platform service can skip even that and install
requestsigning/http's middleware on the callback route.

# Ordering

Deliveries sharing an OrderingKey reach a given endpoint in dispatch order. The
guarantee lives in the claim predicate: a keyed dispatch is claimable only when
no earlier undelivered dispatch shares its key *and its endpoint*, so at most
one is ever in flight per (endpoint, key) across the whole fleet.

The endpoint is in that tuple deliberately. Keyed on the ordering key alone, one
subscriber timing out on resource-42.updated would hold back every other
subscriber's copy of the same event — a dead endpoint stalling healthy ones,
which is the failure per-endpoint circuit breaking exists to prevent,
reintroduced in the claim predicate.

Deliveries with no ordering key are unordered and claim freely.

# Delivery semantics

At-least-once, and it cannot be otherwise: the subscriber and the database have
no shared commit, so a crash between a 200 response and the row update
redelivers on restart. Subscribers must tolerate duplicates, and
DeliveryIDHeader is the key to deduplicate on — it is stable across every
attempt and every replay of one delivery.

Failures back off exponentially with full jitter via retrycfg.DelayFor, persisted
as a timestamp so the schedule survives a restart. Past Backoff.MaxAttempts the
dispatch is marked dead: skipped by every future claim, counted, and left for an
operator to replay. Without that terminal state one permanently broken
subscriber blocks its ordering key forever.

Some failures skip the budget entirely. A 4xx other than 408 or 429 means the
subscriber understood and refused, and a URL that no longer passes
CheckEndpointURL will not start passing; both are marked retry.Unretryable and
go straight to dead rather than spending twenty-five attempts proving it.

Circuit breaking is the other direction. A short-circuited delivery is a failure
but is explicitly *not* charged an attempt — an endpoint down for an hour would
otherwise exhaust the budget of every delivery queued behind it, and they would
all need replaying by hand once it recovered.

# SSRF

An endpoint URL is attacker-supplied and this package makes authenticated
requests to it, which is textbook server-side request forgery: point it at
169.254.169.254 and the worker fetches cloud credentials on the attacker's
behalf. CheckEndpointURL enforces https and refuses any host resolving into
loopback, link-local, private, or non-global space — at registration, where the
rejection can be reported to whoever submitted it, and again at delivery,
because DNS is mutable.

Redirects are refused rather than followed. Following one would deliver a signed
payload to a host that was never registered and never checked, turning an open
redirect on any subscriber's domain into a way to point the worker anywhere.

# Persistence

Store is the seam. This package ships a SQL implementation (NewSQLStore) and the
DDL it needs (webhooks/migrations), so adopting webhooks does not mean writing
one — but an application with its own schema conventions can implement the
interface instead of forking the package.

The five tables are rendered from one prefix rather than five names. They
reference each other by foreign key and the queries join across them, so a
consumer who could name them independently could also name them inconsistently,
and nothing would catch it until the first dispatch.

# The catalog

Subscribable event types are supplied at construction via WithCatalog, not
stored. What an event means is an application opinion and this package has none.

Both Register and Dispatch reject a type outside the catalog. That matters
because an event type is a string and strings are typo-prone: a subscription to
"reciped.created" accepted silently produces an endpoint that never fires, and
diagnosing it means noticing an absence.

# Watching it

The two that matter most are webhooks_backlog_depth and
webhooks_backlog_age_seconds, sampled on the reap tick. Every other instrument
is a rate or a latency, and none of them separates "delivering steadily" from
"delivering steadily while falling further behind" — only the age does.

The rest: webhooks_deliveries_dispatched against webhooks_deliveries_sent (the
gap is the rollback rate), webhooks_deliveries_failed,
webhooks_deliveries_short_circuited, and webhooks_deliveries_dead — alert on any
increase in the last, since a dead dispatch is an event a subscriber will never
see — plus webhooks_claim_errors, webhooks_dispatches_reaped, and the
webhooks_delivery_latency_ms, webhooks_cycle_latency_ms, and
webhooks_claimed_batch_size distributions.

Per-delivery measurements carry an endpoint attribute, because one worker serves
every subscriber and a single broken one is invisible in the total. That
attribute's cardinality grows with the endpoints table; an operator with enough
subscribers to care should drop it in their collector rather than lose the
distinction at the source.

Spans cover Dispatch, each claim, and each delivery. A cycle that claims nothing
is not traced: a root span every poll interval is noise.
*/
package webhooks
