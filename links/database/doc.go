/*
Package database stores action link records in a SQL table.

It is the store — there is no other, and links/doc.go records what the
cache-backed one could not do. A link is minted by whatever builds the email and
redeemed by whatever serves the click, and those are routinely two processes;
without a store both of them can reach, a link is not less durable, it is
unredeemable. A table is a store both of them already have.

	store, _ := linksdatabase.New(&cfg.Database, db,
		linksdatabase.WithSweeper(ctx, 5*time.Minute))
	minter, _ := links.NewMinter(store, links.WithAction(...))

The table is not created for you. Hand migrations.SQL to your own migration run;
see links/database/migrations for why the platform ships no numbered file.

# No locker, and that is the point

A store that cannot make a read and a write one operation needs a
distributedlock.ScopedLocker to stand in for one, and without mutual exclusion
two requests carrying one token both see the link active. Here single use is the
affected row count of an UPDATE guarded on resolved_at IS NULL, inside one
transaction: the server evaluates "this link, and it is still unresolved" at the
instant the row changes, and exactly one of two concurrent redemptions is told
it won.

So a deployment adopting links needs neither Redis nor a lock service, and the
guarantee is stronger rather than merely cheaper — it is decided by the same
server that holds the row.

The count discriminates on all three engines. MySQL reports rows changed rather
than matched, which makes a zero count ambiguous for a statement that might
write values a row already held; this one always moves resolved_at from NULL to
an instant, so a row it matched is a row it changed.

# What is stored, and what is not

Not the token. The primary key is the SHA-256 digest of it — the same ID Mint
returned and Revoke takes — so a backup, a replica, or a support engineer's
query yields a list of links that were issued rather than a set of live
credentials.

There is no tenancy scope column, by decision rather than by deferral. The
module's rule exists to stop a read that forgot the scope from matching
everything, and that needs a read which can widen; every statement here is keyed
by a primary key that is a digest of thirty-two bytes of randomness, so the only
read this table has is the one row nobody can guess the name of. On the
redemption path the column would add nothing besides: whoever holds the token
holds the credential, and requiring a scope only obliges the redeemer to know a
second fact, which a magic-login link — whose purpose is to identify a caller
who is not yet known — cannot supply. The reads that would enumerate want the
subject, which is a column here already: revoking every live link for a person
should cross that person's tenants rather than stop inside one, and
dataprivacy.Eraser is keyed on a subject throughout this module. What this table
holds is a credential, not a domain record, and the rule governs domain records.

# Withdrawing a person's links

RevokeForSubject is on links.Store because subject is a column here, and a
store that cannot read by one is not a store this package accepts. It is
indexed alongside resolved_at, so
"withdraw everything this person still has outstanding" is one UPDATE guarded on
resolved_at IS NULL, reporting the rows it moved. There is no scope argument and
there is no scope column: the write crosses whatever tenants the subject belongs
to, which is what a completed password reset, a locked account, and an erasure
each want.

It runs outside a transaction, because one statement already is one, and it
races a redemption of one of the same links the way two redemptions race each
other: the row's own guard decides, exactly one write moves it, and the count
this reports excludes anything a redeemer took first.

One consequence is worth knowing before you read a row back. This statement
carries no liveness predicate — nothing in links decides liveness in SQL — so a
link that expired without ever being resolved is moved too, and afterwards
answers "revoked" rather than "expired" with its purge_after re-stamped from the
revocation. Both sentences are true of that link; the operator asking for the
revocation is the one whose reading this keeps.

# Expiry, retention, and the sweep

expires_at decides redeemability and purge_after decides collectability, and
they are different instants — the second is later by the Minter's retention
window. That gap is what lets a second click be told "this link has already been
used" instead of "no such link".

Neither is enforced by this package. links.Record.Usable compares expires_at
against the Minter's clock, so a row the sweeper has not reached is already
refused — the decision is Go's, above the store, and no engine's idea of "now"
enters it.

Sweep is what keeps the table sized by live links rather than by every link ever
minted, which is the one piece of housekeeping storage with its own expiry would
have done for itself. Run it, either through
WithSweeper or from a scheduler calling Sweep; a fleet wants the second, so
there is one sweeper rather than one per replica. Its horizon is bound from this
store's clock rather than read off the server, because purge_after was stamped
by a clock and CURRENT_TIMESTAMP would be a different one. See
querygen.AtMostArgument.

One consequence of the dialect tier is visible on SQLite and nowhere else. That
engine has no date type, so a timestamp is text, and the generated querier binds
one in the shape the engine's own CURRENT_TIMESTAMP writes — whole seconds. A
link's stamps are therefore stored truncated to the second there, which moves
every deadline computed from them at most one second earlier. Earlier is the
safe direction for a credential, and the other two dialects store what they are
given.

# Encoding

Link metadata goes into the metadata column through an encoding.Codec, JSON by
default — the column holds a flat map of strings, so an operator answering "what
was this link for" reads it with the database's own JSON functions. A link
carrying no metadata stores NULL.

Rows written with one encoding are unreadable through another and carry no
record of which wrote them, so changing WithCodec on a deployed store
invalidates every outstanding link that carries metadata.
links.RecordVersion cannot soften that: it is a column, not part of the blob, so
it catches a changed record shape and not a changed encoding.
*/
package database

//go:generate go run ./internal/queriesgen
