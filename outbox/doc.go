/*
Package outbox makes "write a row and publish an event" atomic.

Without it those are two independent operations against two systems that share
no commit: the transaction succeeds, the publish fails, and durable state and
the event stream diverge permanently with nothing to detect it. The outbox
closes that gap by writing the event into the same transaction as the state
change, and moving it to the broker afterwards from a separate loop.

# The seam

database.Client.WithTransaction hands its callback an executor and nothing
else — it cannot commit or roll back. Enqueue takes that executor, so the
event is just another statement in the caller's transaction and lives or dies
with it:

	err := client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
		if err := insertOrder(ctx, q, order); err != nil {
			return err
		}

		return writer.Enqueue(ctx, q, outbox.Message{Topic: "orders", Payload: order})
	})

There is no way to enqueue outside a transaction by accident: holding a
SQLQueryExecutor from WithTransaction means you are already in one.

# Creating the table

outbox/migrations renders the DDL for a dialect and table name. If you already
run database/migrate, pass migrations.SQL to WithGeneratedMigration and the
table is created by your normal migration run at a version you choose — no DDL
copied into your repository. Statements returns the same DDL pre-split for
callers using something else.

# Wire compatibility

Payload is marshaled to JSON at enqueue and republished as json.RawMessage,
which marshals as its own bytes. The message the broker receives is therefore
byte-identical to what a direct messagequeue.Publish of the same value would
have produced, so adopting the outbox needs no consumer change.

That identity depends on the publisher encoding JSON — messagequeue/kafka
constructs its encoder with encoding.ContentTypeJSON. A publisher built with a
non-JSON ClientEncoder would double-encode the payload, so the Relay is
JSON-only by contract.

# Delivery semantics

At-least-once, and it cannot be otherwise: the broker and the database have no
shared commit, so a crash between a successful publish and the row update
redelivers on restart. Consumers must tolerate duplicates. Because
messagequeue.Publisher carries no header channel, the outbox cannot attach a
deduplication key without wrapping the payload and breaking the wire
compatibility above — so callers who need dedupe put their own idempotency key
inside the payload.

Ordering depends on the claim mode and on Message.Key. ClaimSkipLocked lets
several relays run concurrently and interleave batches, which gives up global
ordering. Per-key ordering survives regardless, because the claim predicate
admits a keyed message only when no older message with that key is still
pending: at most one message per key is ever in flight across the whole fleet,
so its successor cannot be published until it lands. Unkeyed messages skip that
check and claim freely. ClaimLease serializes on a single relay and preserves
global order. Postgres and MySQL support both modes; SQLite has no SKIP LOCKED
and always uses ClaimLease.

# Watching it

Nothing here fails loudly — publish errors are logged and retried, never
surfaced — so the instruments are how you learn the outbox has stopped working.
Pass WithRelayMetricsProvider and WithWriterMetricsProvider.

The two that matter most are outbox_backlog_depth and outbox_backlog_age_seconds,
sampled on the reap tick. Every other instrument is a rate or a latency, and none
of them separates "publishing steadily" from "publishing steadily while falling
further behind" — only the age does. A depth of 40,000 is unremarkable if the
oldest message is four seconds old and an incident if it is four hours old.
Quarantined rows are excluded from both, so a permanently broken message does not
read as a permanently growing backlog.

The rest: outbox_messages_enqueued against outbox_messages_published (the gap is
the rollback rate), outbox_messages_failed, outbox_messages_quarantined — alert on
any increase, since a quarantined message is a dropped event — outbox_claim_errors,
outbox_messages_reaped, and the outbox_publish_latency_ms, outbox_cycle_latency_ms
and outbox_claimed_batch_size distributions. Everything per-message carries a topic
attribute, because one Relay serves every topic and a single broken publisher is
invisible in the total.

Spans cover Enqueue, each claim, each publish, and each reap. A cycle that claims
nothing is not traced: a root span every poll interval is noise.

# Failure

A message that cannot be published has its attempt count incremented and its
next attempt pushed out by exponential backoff. Past MaxAttempts it is
quarantined: skipped by every future claim and counted by
outbox_messages_quarantined. Without that terminal state a single
permanently-failing message blocks the head of the queue forever, which is the
failure this package is most likely to actually meet.

Published rows are marked rather than deleted, so a duplicate or a gap can be
investigated after the fact, and a reaper deletes them once they age past
Retention.
*/
package outbox
