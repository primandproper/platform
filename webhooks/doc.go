/*
Package webhooks delivers outbound webhooks: signed, retried, ordered, and
replayable.

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

Both the scheme and the timestamp are inside the signed material, and each is
load-bearing.

The timestamp is what makes a captured request expire. A signature over the body
alone is valid forever, so anyone who observes one request can replay it against
the subscriber indefinitely. Verify rejects anything outside DefaultTolerance,
and does so before computing any HMAC, so a replay flood costs the subscriber
nothing.

The v1 prefix is what makes the construction replaceable. Binding the scheme
into the signed bytes means a v1 signature can only ever verify as v1, so a
later scheme can be introduced alongside it rather than by flag-day.

Rotation is the other half. Secret carries Current and Previous, and every
delivery is signed under both while Previous is set — several s= components in
one header. A subscriber rolls its key by accepting either signature for as long
as it needs, and the operator clears Previous afterwards. A single secret per
account, which is what this package was extracted to replace, cannot be rolled
at all without breaking every subscriber for that account simultaneously; in
practice that means it never gets rolled.

Verify ships here on purpose. Verification is where these schemes are actually
got wrong — subscribers compare with ==, skip the timestamp check, or verify a
re-serialized body rather than the received bytes. Handing out the sender and
leaving the receiver to reimplement it from prose is how that keeps happening.

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
requests to it, which is textbook server-side request forgery: point it at an
internal address and the worker reaches something the attacker cannot.

Worth sizing honestly, because the standard telling of this inflates it. The
response body is read into io.Discard and never surfaces anywhere, so this is a
blind SSRF — an oracle for mapping internal address space out of status codes,
plus whatever a POST to one of those addresses sets in motion. Not credential
theft: the metadata service can be reached but not read. And registration is
normally behind a permission, so the whole thing sits behind an authenticated
account rather than the open internet.

Still worth closing, and CheckEndpointURL closes it: https only, and no host
resolving into loopback, link-local, private, or non-global space — at
registration, where the rejection can be reported to whoever submitted it, and
again at delivery, because DNS is mutable.

Checking is not enough on its own, because resolution and connection are two
separate lookups and an attacker who controls the authoritative server answers
them differently: public for the check, 169.254.169.254 for the dial moments
later. So the worker does not let the second lookup happen. The delivery-time
check reports the addresses it approved, they ride the request's context, and
PinningDialContext connects to one of them and refuses everything else. TLS
still verifies against the hostname, so pinning costs the subscriber's
certificate nothing.

That enforcement follows the policy rather than overriding it. A deployment that
replaced the checker with WithWorkerURLChecker — the sidecar case, where
delivering to a private address is the point — vets no addresses and so pins
nothing; one that wants both writes a PinningURLChecker instead. The pin also
needs to reach the dialer: a client supplied through WithHTTPClient is pinned
when its transport is an *http.Transport, and a transport wrapped in
instrumentation wants the pin installed underneath it, which is what
httpclient.WithDialWrapper is for and what webhookscfg does.

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
