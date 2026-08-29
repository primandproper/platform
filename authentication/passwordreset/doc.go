/*
Package passwordreset stores the token that lets somebody who cannot sign in
prove they own the address the account was registered with.

This module already ships every other piece of the flow: argon2 hashes the
password that replaces the old one, email sends the link, identity holds the
user the link resolves to. What it shipped nothing for was the middle — the row
recording that a reset was asked for, whom for, until when, and whether it has
already been used — so every consumer wrote it, and identity's own
documentation named this package as the home of a table that did not exist.

# Why it is not four columns on the user

A reset token is a set per user rather than a column on one, it has a lifecycle
of its own, and it is consumed by exactly one flow. Those are the three tests
identity applies to decide what lives on the user row and what lives beside the
engine that reads it, and this fails all three: an application that issues a
second link before the first expires has two live tokens, an expiry and a
redemption are states nothing else on the user has, and no read outside this
flow ever wants them.

# The three things a hand-written one gets wrong

Everything here exists because a reset token store is security-sensitive
boilerplate — code that is short enough to look like it does not need a library
and dangerous enough that the mistakes are vulnerabilities rather than bugs.

The token is stored as a digest and never as itself. What goes in the column is
[github.com/primandproper/platform-go/v13/cryptography/hashing.Hasher] applied
to the secret, hex-encoded; the secret exists once, in the [Issuance] returned
by [Store.Issue], and this package never has a place to put it again. That is
what makes a database copy — a backup, a read replica, a support engineer's
SELECT — not a password reset for every account with an outstanding link. It is
unsalted, deliberately: what is digested is thirty-two bytes from a CSPRNG
rather than something a person chose, so there is no dictionary to run against
it and a salt would only cost the indexed lookup.

Single use is enforced by the store rather than by the caller. [Store.Consume]
reads the row and stamps its redemption inside one transaction, and it is the
stamp's affected-row count — not the read — that decides who owns the token. Two
requests answering one link at the same instant both find the row live; only one
of their updates reports a row, and the other is told the token has already been
redeemed. Doing it in two statements, or deciding on the read, leaves a window
exactly as wide as the password write that follows, and a link that resets a
password twice is a link an attacker can race.

Expiry is refused rather than swept. A row past its deadline is dead to
[Store.Verify] and [Store.Consume] whether or not anything has deleted it yet,
so a deployment that never runs the sweep has a table that grows rather than
links that outlive their TTL.

# Verify and Consume are two calls because they answer two moments

[Store.Verify] is the page load — somebody followed the link and is about to be
shown a form — and it spends nothing, so a user who opens the link, thinks
better of it, and comes back an hour later still has the token they had.
[Store.Consume] is the submit. They return the same [Token] and refuse for the
same three reasons ([ErrTokenNotFound], [ErrTokenExpired], [ErrTokenRedeemed]),
which is deliberate: telling somebody holding an expired link that it expired is
worth more than the nothing an attacker learns from it, since learning it
requires already holding the token.

The order a caller runs them in is the one that fails safe. Consume, then write
the new password: a redemption that succeeds and a password write that fails
costs the user another email, while a password write that succeeds and a
redemption that fails leaves a live reset link for an account whose password has
just changed. [Store.RevokeForUser] is what a completed reset calls afterwards,
so the other links that were outstanding stop working too.

# This is not links, and the difference is the table

[github.com/primandproper/platform-go/v13/links] mints single-use, expiring URLs
for four flows and names password reset as one of them. It digests its token,
refuses a replay, and separates Inspect from Redeem exactly as this package
separates Verify from Consume, so the question of which one an application wants
is a fair one and the answer is not "whichever you find first".

links is a cache. Its records live in a
[github.com/primandproper/platform-go/v13/cache.Cache], Redis in production, and
its single-use guarantee is a
[github.com/primandproper/platform-go/v13/distributedlock] lock held across the
read and the write — which is why the locker is a required argument there with
no default. It mints whole URLs from a registry of action policies, so one
primitive serves magic login, unsubscribe, and verification without knowing what
any of them means.

This is a table, and three things follow from that. Single use is the affected
row count of a guarded UPDATE inside one transaction, so it needs no lock
service and holds when Redis is not in the deployment at all. Every row carries
a [github.com/primandproper/platform-go/v13/tenancy.Scope], which links has no
notion of. And a reset token is stored against a user rather than an opaque
subject, which is what makes [Store.RevokeForUser] a statement — the question
links documents as unanswerable from its own store, needing an audit-log query
and a Revoke per result, and the question a completed password reset has to ask
every time.

So: an application that wants one primitive for its four link flows, and is
running Redis, wants links. An application that wants password reset to be a
durable, tenant-scoped, revocable-per-user fact in the same database as its
users — surviving a cache flush, and answerable by a report — wants this. Nothing
stops a deployment using both for different flows; they share no state and no
table.

# Tenancy

Every row carries a
[github.com/primandproper/platform-go/v13/tenancy.Scope] and every statement
binds it — the module's rule, not an exception to it. A token identifies a
principal in a scope, and there is no unscoped read: an application with one
directory passes
[github.com/primandproper/platform-go/v13/tenancy.Global] everywhere and
behaves exactly as an unscoped store would.

The store's own machinery is the one exception, and it is narrow. [SQLStore.Sweep]
spans every scope because one scheduler reclaims one table for the whole
deployment, and it deletes by deadline rather than answering a read.

# The table is yours to create

[github.com/primandproper/platform-go/v13/authentication/passwordreset/migrations]
renders the DDL for a dialect and prefix. Nothing here creates a table on its
own: a library that ran DDL against a caller's database would be a library that
decided when a deployment's schema changed.

# The sweeper

Rows expire, but a table does not reclaim them. [WithSweeper] starts a
background delete of everything past its deadline; without it, and without a
scheduler calling [SQLStore.Sweep], the table grows by a row for every password
anybody ever forgot — including the requests nobody followed up, which are the
ones no redemption ever removes.

# Where the SQL comes from

Every statement this package executes is generated. The table's facts — its
name, its columns in projection order, and which of them a write assigns — are
spelled once, in internal/queries. `make generate` renders them through
database/querygen into the canonical .sql files beside that package, in sqlc's
spelling: named statements whose arguments are sqlc.arg references. `make
sqlc_compile` checks every one of them against the DDL migrations produces, on
all three dialects, with no database running; sqlc-gen-unison emits
internal/passwordresetdb from the same files — typed params and methods over
driver placeholders — and that is what the store executes.

So a column renamed in the DDL is a failed generate rather than a scan error at
run time, and the pairing between what a SELECT projects and what a Scan reads
is generated rather than maintained by eye. What this package writes by hand is
which statements it wants; it writes no SQL.

Three of this store's decisions are not consequences of the shapes those
statements are rendered from, and each is argued where it now lives. The
projection excludes token_digest, so nothing hands a caller a stored
credential's digest. Every instant is bound in UTC, which is what makes the
sweep's comparison chronological on SQLite, where a DATETIME column holds text.
And liveness is compared in Go rather than in a predicate: the sweep deletes
rows dead by any reading, but the boundary a user hits at the last second of a
link's life is decided against one clock, in one place, on all three engines.
*/
package passwordreset

//go:generate go run ./internal/queriesgen
