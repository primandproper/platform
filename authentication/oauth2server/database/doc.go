/*
Package database keeps an authorization server's state in SQL tables.

	store, _ := database.NewStore(&database.Config{}, db,
		database.WithSweeper(ctx, oauth2server.DefaultSweepInterval))

	srv, _ := oauth2server.NewServer("https://auth.example", store, authenticator)

This is the implementation a deployment wants. The alternative — four maps
behind an RWMutex — is what a consumer assembling an authorization server from
the reference examples writes, and it works perfectly on one replica.

# What durability actually buys here

Three specific things, and none of them is "data survives a restart" in the
abstract.

An authorization code is written by whichever replica served /authorize and
read by whichever replica serves /token, a few hundred milliseconds later. With
per-process state those are the same replica or the login fails — so a fleet
behind a load balancer fails logins in proportion to how evenly the balancer
spreads them, which presents as a flaky login rather than as a missing
dependency.

A registered client outlives a deploy. Under RFC 7591 dynamic registration
every client is a registered client, so per-process state means a deploy
invalidates the entire client population at once.

A revocation is enforceable. Access tokens here are opaque and checked against
this store on every resource-server request, so a sign-out ends a session now
rather than at the end of the token's lifetime.

# Four tables

	<prefix>oauth2_clients               registrations, with an optional expiry
	<prefix>oauth2_authorization_codes   one row per issued code, spent once
	<prefix>oauth2_access_tokens         opaque tokens, by digest
	<prefix>oauth2_refresh_tokens        rotating tokens, grouped by family

The DDL ships in database/migrations, rendered for postgres, mysql, and sqlite;
hand SQL to database/migrate's WithGeneratedMigration and the tables are created
by the consumer's own migration run.

Every credential table keys on a hex SHA-256 digest, never on the credential. A
dump of this database therefore contains nothing that can be redeemed — a
property the map-backed store gets for free by dying with the process, and the
one that most obviously stops being free the moment the store is a table.

# The two statements that carry the design

Consuming an authorization code is a single guarded UPDATE:

	UPDATE ...codes SET redeemed_at = $now
	 WHERE hash = $hash AND redeemed_at IS NULL AND expires_at > $now

Both predicates are load-bearing. `redeemed_at IS NULL` is what makes two
concurrent redemptions of one code resolve to exactly one winner; `expires_at >
$now` closes the window in which a code expires between a read and the write
that follows it. A store that checked either of those in Go would have both
races, and neither would show up in a test that redeems one code at a time.

Rotating a refresh token is the same statement with `revoked_at IS NULL` added,
which is what keeps a revoked token from reading as a replay — it was never
exchanged, so reporting reuse would revoke a family every time somebody signs
out and their client retries.

A SELECT follows each UPDATE inside the same transaction, but only to explain
what the UPDATE already decided: whether zero rows affected meant absent,
replayed, or expired.

# Nullable timestamps

expires_at on a client, redeemed_at on a code or refresh token, and revoked_at
on either token are all nullable, and the NULL is the point. It distinguishes
"never expires" from "expired at the zero time", and lets every predicate above
be an `IS NULL` rather than a comparison against a magic date.

# Sweeping

Nothing reclaims these rows on its own. WithSweeper runs one, or a scheduler
calls Sweep — which is the better answer for a fleet, since it is one sweep
rather than one per replica. Without either, the authorization code table grows
by one row per login attempt forever, and the client table grows by one row per
anonymous registration.

A revoked token that has not yet expired is deliberately kept: a resource server
holding one is entitled to be told "no" rather than to have its request read as
carrying a token nobody ever issued.

# Conformance

This store and the memory one are held to the same suite,
authentication/oauth2server/oauth2servertest, including the concurrent
redemption and expiry-between-read-and-write cases. That is the only thing that
says a guarded UPDATE and a mutex mean the same thing.
*/
package database
