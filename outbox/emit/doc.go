/*
Package outboxemit makes every event a write owes unforgettable.

outbox closes the dual write for one event: Enqueue takes the executor
WithTransaction hands its callback, so the event is another statement in the
transaction that wrote the row. A write usually owes more than one. A single row
change can owe a data-change message on the broker, a search index event so the
index does not go stale, and a webhook dispatch row per subscribed endpoint —
all three in the same transaction, for the same reason the first one is.

Left as three calls, they are three things an author has to remember, and the
one that gets forgotten is not detectable afterwards. A missing index event is
an index that is wrong until the reindex backstop runs, with nothing in between
able to tell that it is. This package makes them one call:

	err := client.WithTransaction(ctx, func(q database.SQLQueryExecutor) error {
	    if err := updateSetting(ctx, q, setting); err != nil {
	        return err
	    }

	    return emitter.Emit(ctx, q, DataChange{Type: "setting.updated", ID: setting.ID},
	        outboxemit.WithIndexUpsert("settings-index", setting.ID),
	        outboxemit.WithOrderingKey(setting.ID))
	})

One call, inside the transaction, fanning out to everything the write owes, in
one Enqueue and therefore one round trip.

# Wiring

	emitter, err := outboxemit.NewEmitter[DataChange]("data_changes", writer,
	    outboxemit.WithSideEffect("webhooks", dispatchWebhooks),
	    outboxemit.WithLogger(logger),
	    outboxemit.WithTracerProvider(tracerProvider),
	    outboxemit.WithMetricsProvider(metricsProvider))

writer is the outbox.Writer, built from outboxcfg. The topic is where the
caller's own messages go; index events name their own, since which index a
document belongs to is carried by the topic and one write can touch several.

The message type is the only genuinely consumer-shaped part of any of this, so
it is the type parameter and this package holds no opinion about its contents.
It never looks inside one — the outbox marshals it, and pins JSON.

# The registration seam

Data-change messages and index events are things this module already owns, so
they are options. Webhook dispatch is not: what an endpoint subscription looks
like, and what a dispatch row holds, are the application's entirely. It plugs in
as a SideEffect, registered once at construction and run on every Emit against
the caller's executor:

	func dispatchWebhooks(ctx context.Context, q database.SQLQueryExecutor, msg DataChange) ([]outbox.Message, error) {
	    endpoints, err := subscribedEndpoints(ctx, q, msg.Type)
	    if err != nil {
	        return nil, err
	    }

	    for _, endpoint := range endpoints {
	        if err = insertDispatch(ctx, q, endpoint, msg); err != nil {
	            return nil, err
	        }
	    }

	    return nil, nil
	}

Registration is construction-time and not per-call, which is the whole point. A
side effect every write owes is a property of the wiring; if a call site could
decline it, the call site that forgets to ask for it is back.

A side effect may write rows, return further outbox messages, or both. An error
from one aborts the emission, and the caller's transaction rolls back — taking
the row change and every message with it. That is the right outcome: a write
whose side effects cannot all be recorded is a write that should not have
happened.

# The index event names a document; it does not carry one

WithIndexUpsert and WithIndexDelete write a searchsync.Event, which holds a
document ID and an operation and nothing else. Whenever a Syncer applies it, and
however many times, it reads the row back and indexes its current state — so
redelivery and out-of-order delivery both converge, and an upsert whose row has
since been deleted is applied as a delete rather than stranding a document
nothing will mention again. searchsync's documentation has the long form.

# The document ID is the ordering key

Every index event is keyed by its document ID. That is what buys per-document
ordering: the outbox admits a keyed message only when no older message with that
key is still pending, so at most one event per document is in flight across the
whole relay fleet, however many relays are running.

It is a one-line convention with no obvious home, and getting it wrong produces
reordered index writes under relay concurrency — a failure that appears only
under load and only sometimes. So it is not a convention here: WithIndexUpsert
and WithIndexDelete apply it, and there is no way to write an unkeyed index
event through them.

WithOrderingKey does the same for the caller's own message, and is separate
because the two need not agree — one data-change message can carry index events
for several documents.

# Watching it

Pass the metrics provider. The trio — outbox_emit_requests, outbox_emit_errors,
outbox_emit_latency_ms — says whether emissions are happening and succeeding.

outbox_emit_fanout is the one this package exists for: how many messages one
write produced. A call site that stopped asking for its index event shows up as
a drop in that distribution and nowhere else, because the events it no longer
writes are events nothing downstream will miss. Everything carries the topic,
because one process runs one Emitter per message type.

Read it beside the outbox's own instruments, which answer the next questions
along: outbox_messages_enqueued against outbox_messages_published for whether
the relay is keeping up, outbox_backlog_age_seconds for how far behind it is,
and search_sync_lag_ms for whether the index has caught up with either.

Spans cover each Emit, carrying the topic, the ordering key, the message count,
and the names of the side effects that ran — so a transaction that fanned out to
three destinations is legible from the trace alone.

# There is no config subpackage

Every other assembly seam in this module has one because there is something to
select from the environment: a provider, a connection string, a credential.
There is nothing of the kind here. The message type and the side effects are
application code, the topic is a constant beside them, and the outbox this
writes through comes from outboxcfg already. A config package here would add a
second name for one string.
*/
package outboxemit
