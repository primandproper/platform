/*
Package database stores action link records in a SQL table.

It is the store for a deployment that has a database and does not have Redis —
which, for links, is not a small-deployment compromise. A link is minted by
whatever builds the email and redeemed by whatever serves the click, and those
are routinely two processes; without a store both of them can reach, a link is
not less durable, it is unredeemable.

	store, _ := linksdatabase.New(&cfg.Database, db,
		linksdatabase.WithSweeper(ctx, 5*time.Minute))
	minter, _ := links.NewMinter(store, links.WithAction(...))

The table is not created for you. Hand migrations.SQL to your own migration run;
see links/database/migrations for why the platform ships no numbered file.

# No locker, and that is the point

links/cache needs a distributedlock.ScopedLocker, because a cache cannot make a
read and a write one operation and without mutual exclusion two requests
carrying one token both see the link active. Here single use is the affected row
count of an UPDATE guarded on resolved_at IS NULL, inside one transaction: the
server evaluates "this link, and it is still unresolved" at the instant the row
changes, and exactly one of two concurrent redemptions is told it won.

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

There is no tenancy scope column, and that is the one place this table departs
from the module's rule. A link is never read by enumeration and never by
anything but the bearer's own digest, and links.Mint takes no scope to bind:
adding the column means changing that signature, which is a decision of its own
rather than a consequence of moving the records into a table.

# Expiry, retention, and the sweep

expires_at decides redeemability and purge_after decides collectability, and
they are different instants — the second is later by the Minter's retention
window. That gap is what lets a second click be told "this link has already been
used" instead of "no such link".

Neither is enforced by this package. links.Record.Usable compares expires_at
against the Minter's clock, so a row the sweeper has not reached is already
refused, and the same comparison answers the same way in links/cache where there
is no server to ask.

Sweep is what keeps the table sized by live links rather than by every link ever
minted — the one thing a cache does for itself. Run it, either through
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
