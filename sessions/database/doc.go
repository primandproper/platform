/*
Package database stores session records in a SQL table.

Reach for it when losing the cache must not sign everybody out, or when a
sign-out has to be enforceable rather than very nearly enforceable. Otherwise
sessions/cache is cheaper and does the same job.

	backend, _ := sessionsdatabase.NewBackend[Principal](
		&sessionsdatabase.Config{TablePrefix: "ddb"}, db,
		sessionsdatabase.WithSweeper(ctx, 5*time.Minute),
	)
	store, _ := sessions.NewStore(backend)

The dialect is taken from the database.Client rather than configured. A
configured dialect that disagrees with the client it is paired with produces
syntactically valid SQL the server rejects at runtime, and there is no way to
notice before it does.

# What the table buys

Two operations are single statements here and approximations in the cache
backend, and both matter for the same reason — a request that read a session
just before it ended must not be able to write it back afterwards:

Update is one UPDATE with a WHERE clause. A row that has been deleted is not
recreated, so a sign-out that has committed stays committed.

Rename runs its DELETE and INSERT in one transaction. Either the old identifier
stops resolving and the new one starts, or neither does. There is no interval in
which both work, which is the interval a session-fixation attack needs.

# Sweeping

A cache reclaims its own expired entries; a table does not. Without a sweep this
one grows with every session ever created — the rows are unusable long before
they are removed, since the store decides expiry from the record's own anchors,
but they are still there.

WithSweeper runs it in-process on a timer. In a fleet that is one sweeper per
replica doing the same delete; a scheduler calling Sweep once is the better
shape, and the reason Sweep is exported.

# The schema

The DDL lives in the migrations subpackage, rendered for a dialect and a table
prefix. It is not shipped as a numbered migration file — those are numbered
globally per consumer, so a platform-owned number would collide with theirs.

expires_at exists for the sweeper alone and appears in no read predicate. Which
sessions are live is decided above this layer, from created_at and last_seen_at
against the store's policy, so that both backends answer the question
identically — and so that clock skew between a writer and a reader cannot hide a
live session.

created_at appears in no UPDATE. It anchors the absolute timeout, and leaving it
out of the statement makes "an update never extends a session's total lifetime"
structural rather than a rule somebody has to remember.

# Encoding

Payloads go into the data column through an encoding.Codec, CBOR by default. A
session with no payload stores NULL and reads back as nil rather than as a zero
value.

Rows written with one encoding are unreadable through another and carry no
record of which wrote them, so changing WithCodec on a deployed store signs
everybody out. Record.Version cannot soften that: it is a column, not part of
the blob, so it catches a changed payload shape and not a changed encoding.
*/
package database
