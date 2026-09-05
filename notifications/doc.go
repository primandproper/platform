/*
Package notifications is the durable half of telling somebody something: the
in-app inbox, and the registry of devices a push can be addressed to.

The subpackages beside it deliver. notifications/mobile sends to handsets
through APNs and FCM; notifications/async pushes to a connected browser through
Ably, Pusher, SSE or a websocket. Neither stores anything, which left every
consumer writing the same two tables: the row that says "this person was told
this, and has or has not read it", and the list of tokens to push to.

# The two halves, and why the registry is the one that goes wrong

The inbox is the generic half. Created, read, archived is the same lifecycle in
every application, and what varies — the topic, the wording, the link — is
opaque to this package.

The registry is the half with a feedback loop, and it is why this lives beside
the senders rather than in each consumer. Providers report invalid and expired
tokens on send: a phone is wiped, an app is uninstalled, a token is reissued,
and the next push comes back Unregistered. A registry that never hears about
that keeps addressing pushes to handsets that no longer exist, indefinitely,
while every send reports a failure nobody connects to a row anybody could
delete. So a rejection the provider calls permanent becomes
mobile.ErrTokenInvalid at the sender, and [Registry.InvalidateDeviceToken] is
the hook that removes the row:

	store, err := notifications.NewSQLStore(client)
	// ...
	sender := mobile.NewMultiPlatformPushSender(apnsSender, fcmSender,
		mobile.WithTokenInvalidator(store))

Wire it and dead tokens leave on their own. Leave it unwired and the
classification still reaches the caller as an error — nothing is hidden — but
nothing prunes.

From configuration the same wiring is two registrations rather than a call:
notifications/config registers the store, notifications/mobile/config registers
the sender and resolves the registry optionally, and a container carrying both
prunes. A container carrying only the sender behaves exactly as it did before.

# Tenancy, and the one method without it

Every read and write here takes a tenancy.Scope, and the inbox takes a principal
beside it, because a notification addressed to somebody is not a row the rest of
their tenant may read. There is no unscoped variant of anything a consumer
calls.

[Registry.InvalidateDeviceToken] is the exception, and it is the component
servicing itself rather than answering a consumer read. What a provider hands
back is a token and nothing else — not the tenant, not the person — so a scoped
variant would require the caller to already know the answer the hook exists to
act on. The token is unique across the whole registry, so naming it names one
device.

# Where the SQL comes from

The store executes no SQL this module has not checked against its own schema.
notifications/internal/queries describes the two tables as data; a generator
renders that into one .sql per dialect;
sqlc checks each against the DDL notifications/migrations ships; and
sqlc-gen-unison turns the checked statements into the typed querier the store
calls. A column renamed in a migration is a failed generate rather than a
runtime scan error, on all three dialects at once.

	make generate   # re-renders internal/queries/<dialect>_generated.sql
	make unison     # re-renders the schema and the generated querier

# Getting the tables

notifications/migrations renders the DDL for a dialect and a table prefix. It
ships no numbered migration file, because migration numbers are global per
consumer; hand migrations.SQL to database/migrate's WithGeneratedMigration and
the tables are created by your own migration run.

# Where this package stops

At the store and the senders. The inbox endpoints a client polls — list mine,
mark one read, mark them all read — are an application's routes over an
application's types, and this package ships none of them. The module README's
"Transports" section is where that line is drawn for the module as a whole.
*/
package notifications

//go:generate go run ./internal/queriesgen
