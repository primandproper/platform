/*
Package pgnotify turns Postgres LISTEN/NOTIFY into a wake-up signal for a
poller.

It is not a message transport and must never be used as one. NOTIFY is
at-most-once and connection-scoped: a listener that is reconnecting misses
everything sent in the gap, and there is no replay. What it is good for is
telling a loop that already knows how to find its work that there is work now,
so an idle queue drains in milliseconds instead of a poll interval and an idle
process stops issuing a query per tick.

# The shape

A Listener holds one dedicated connection, issues LISTEN, and converts every
notification into a send on a coalescing channel:

	listener, err := pgnotify.NewListener(ctx, &pgnotify.Config{
		ConnectionString: dsn,
		Channel:          "outbox",
	}, pgnotify.WithLogger(logger), pgnotify.WithMetricsProvider(metricsProvider))
	if err != nil {
		return err
	}

	go listener.Run()
	defer func() { _ = listener.Close(ctx) }()

	relay, err := outbox.NewRelay(ctx, cfg, client, provider,
		outbox.WithRelayWakeup(listener.Signal()))

The consumer never learns Postgres is involved: it takes a <-chan struct{} and
can be tested with a bare channel. The producer side is equally thin — one
pg_notify with an empty payload, run on whatever executor is already in hand,
which is what outbox.WithWriterNotifyChannel and workqueue's NotifyChannel
emit.

# Signal, not stream

Signal is edge-triggered and level-collapsed. The channel has capacity one and
the send is non-blocking, so a burst of notifications produces at most one
pending wake, and a consumer that reads it once has absorbed all of them. That
is the correct semantic for a poller: it re-reads the table when it wakes, so
the number of notifications it missed does not matter — only that it wakes.

Payloads are ignored entirely. Notifications carry none, which makes Postgres
collapse duplicate (channel, payload) pairs within a transaction for free, and
means nothing in the system can come to depend on the contents of a signal that
is allowed to be lost.

# Every reconnect is a gap

A session begins with an unconditional signal, the first connect included. The
listener cannot know what was sent while it was away, so it does not try: it
wakes the consumer, and the consumer's own query is what establishes the truth.

# The listener connection does nothing else

Postgres buffers undelivered notifications in a cluster-wide 8 GB async queue.
A listener that is connected but not draining fills it, and at that point
*every committing transaction in the cluster* begins to fail. A slow listener is
a cluster-wide write outage, which is why the loop here hands off to a channel
with a non-blocking send and immediately waits again. It never does work inline,
and a consumer that is behind loses wakes rather than backing up the server.

# Deployment constraints

  - **PgBouncer in transaction or statement pooling mode breaks LISTEN
    outright.** The session is not yours between transactions, so the LISTEN is
    issued on a connection that is handed to somebody else. This is the most
    common way this feature silently does not work. Use session pooling or a
    direct connection.

  - **NOTIFY does not cross replication.** A listener must reach the primary; a
    replica DSN will connect, listen, and never hear anything.

  - **The connection is held for the process's lifetime.** Config takes its own
    connection string rather than borrowing from an existing pool, because
    postgres.PgxAccess's pools cap the union of the native and database/sql
    surfaces — a borrowed listener connection would permanently cost the
    application one pool slot.

# Channel names

A channel name is validated with dialect.ValidIdentifier and bounded by
MaxChannelLength, and the LISTEN it renders is quoted. That quoting is a
correctness requirement, not just an injection guard: pg_notify takes its
channel as text and compares byte-for-byte, while an unquoted LISTEN identifier
would be down-cased. Quoting both sides is what makes "Outbox" on the producer
match "Outbox" on the listener. Prefer lowercase names regardless.

# Watching it

Pass WithMetricsProvider. postgres_listener_notifications_received against
postgres_listener_wakes_coalesced tells you how much a burst is actually
collapsing, and postgres_listener_reconnects against
postgres_listener_connect_errors is how you learn the listener is flapping —
which, because every reconnect fires a catch-up cycle, otherwise looks like a
consumer that is simply busier than usual.

Individual notifications are not traced. A root span per wake would be one span
per enqueue for the whole fleet, and the span that matters — the cycle the wake
triggered — belongs to the consumer.
*/
package pgnotify
