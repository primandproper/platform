/*
Package billing stores what a deployment sells and what its customers paid.

This module has shipped a payments seam for a long time and has never shipped a
place to put the answers. capitalism talks to Stripe and RevenueCat; metering
counts what was consumed; entitlements gates on the result. Between them sat four
tables — a catalog, the recurring agreements, the one-time sales, and the ledger
of attempts — which every consumer wrote by hand, and which are the same four
tables in every one of them. Measured in the consumer this package was written
against, that came to roughly four and a half thousand hand-written non-test
lines, and not one column in them was that application's own.

So this package owns those four, in the same way
[github.com/primandproper/platform-go/v13/identity] owns users: a Store
interface, a SQL implementation of it, the DDL for three dialects, and a mock. A
consumer keeps its service layer, its handlers, its proto, its checkout flow and
every judgement about what a status means; it does not keep a subscriptions
table.

# This reverses a ruling, and the reasoning is worth stating

Until this package existed, two documents in this module said the opposite.
capitalism said the mapping from a processor's status onto an account's standing
is policy that lives with the application; entitlements said the join between an
account and a purchased plan is application data that lives in a column next to
the account. Both are still true, and neither was ever an argument about who owns
the table.

The distinction is the same one identity draws. Registration policy is the
consumer's — whether an invitation is required, what a username may look like —
and the users table is not. Here, which of capitalism's eight statuses leaves an
account entitled is the consumer's, and the row recording which status the
processor reported is not. This package stores facts a payment provider owns and
declines to interpret a single one of them: nothing here reads
[Subscription.Status] and decides anything, and there is no column into which a
deployment's idea of "entitled" could be written.

What that buys is the thing entitlements said it could not have. Its PlanSource
seam is filled by [github.com/primandproper/platform-go/v13/billing/plans], which
is this store's current-subscription read plus a function the consumer writes —
so the policy stays exactly where that package put it, and stops being written
against a hand-rolled table.

# Where the boundary with capitalism falls

capitalism is the wire and this is the record, and neither imports the other's
concerns. [capitalism.PaymentManager] takes a
[capitalism.PaymentIntentCreationInput] and hands back a
[capitalism.PaymentIntent]: arguments to a provider call, gone once it returns.
[Purchase] is what the attempt left behind — an account, a product, an amount, a
provider identifier held as a foreign key rather than as its identity, read back
by the application afterwards. The two overlap in vocabulary and in nothing else.

One type does cross: [Subscription.Status] is [capitalism.SubscriptionStatus],
not a set of this package's own. That is deliberate. capitalism's is already the
closed, documented set every adapter maps its provider's words onto, and a second
enumeration here would be the same judgement — which of Stripe's words is
"cancelled" — made twice, in two places that could disagree. The one status this
package will not store is capitalism's unknown: a provider said something no
adapter could place is a fact worth keeping in a variable, and not one worth
writing into the column entitlement decisions are read from.

# Redelivery is the property this schema is shaped around

Every write here that a payment provider triggers can arrive twice, because every
payment provider redelivers. Three mechanisms answer that, and all three are in
the statement rather than in whatever the caller does next:

The three provider-identifier columns are unique within a scope, so a second
delivery of a charge collides instead of recording it twice — which is the
difference between a ledger somebody can sum and a number somebody reconciles by
hand. Every create is an insert-ignore over that index rather than a plain insert:
the row already there wins unchanged and the affected count is how the caller
learns it lost, so nothing decides the identifier is free in a statement before
the one that uses it. The store reports the loss as [ErrTransactionExists] and its
siblings, so a handler acknowledges the delivery rather than retrying it forever.

A zero count says the row lost and not what it lost to, so the store reads once
more to attribute it, on the losing path and therefore never on the hot one. That
read is also what keeps the three engines saying the same thing: MySQL's IGNORE
covers every constraint on the table rather than the one index, so a create naming
a product nobody has arrives there as a zero count where Postgres and SQLite raise
a foreign key. The two creates that reference a product ask for it before they
insert, and the ledger's create asks about its referents when it loses. See
[ErrIDTaken] for the one case the three engines report differently, and why
nothing but a caller's own bug reaches it.

The two status writes are guarded on the column they assign — `SET status = X
WHERE status <> X` — so a redelivered event touches nothing and is told
[ErrStatusUnchanged], which is an answer rather than a failure.

[PurchaseStore.CompletePurchase] is guarded on completed_at being NULL, so a
purchase completes exactly once and a second delivery cannot restamp the moment
the money arrived.

Each of the three columns is also nullable, and that is what makes the uniqueness
usable rather than an obstacle. A free tier, a comped plan and a subscription
granted by hand have no provider-side counterpart at all, and all three engines
treat NULLs in a unique index as distinct — so the rows that have a provider id
are unique and the rows that do not stay out of each other's way. See
billing/migrations.

# A price is a fact about a moment, not a lookup

[Purchase] and [Transaction] each carry their own amount and currency rather than
reading them through the product. Repricing a product must not rewrite what
somebody already paid, and a partial refund is a transaction whose amount was
never any product's price. A schema that joined to find an amount could hold
neither.

The same reasoning is why there is no statement able to assign an amount. The
only thing that legitimately changes about a ledger row is its status, and the
only thing that changes about a purchase is whether the money arrived.

# Getting the tables

The DDL lives in [github.com/primandproper/platform-go/v13/billing/migrations],
rendered per dialect and table prefix, and hands to database/migrate's
WithGeneratedMigration so nothing is copied into a consumer's repository. See that
package for why no numbered migration file ships.

That package also answers which tables exist, at your prefix, through its Tables
function — the list is complete and read from the DDL, so a between-tests
TRUNCATE, a backup policy or a privacy inventory names every one of them without
anybody copying four names out of the schema.

# What a consumer still writes

The service layer, the checkout flow, and every judgement about what a status
means.

Concretely: the handler that creates a payment intent through capitalism and then
writes the [Purchase] it will settle into; the webhook endpoint that verifies a
signature through webhooks/inbound, maps the payload through a capitalism adapter,
and calls one method here; the function billing/plans takes, saying which
statuses leave an account entitled; and the mapping onto identity.BillingStatus,
which is the coarse standing an application gates on and includes a suspension no
processor reports.

# Subject access, and the erasure that is deliberately absent

[github.com/primandproper/platform-go/v13/billing/privacy] is a
dataprivacy.Collector and no Eraser. Financial records carry a statutory retention
that outranks a right to erasure in every jurisdiction that grants both, so an
Eraser here would be a seam whose only correct implementation erases nothing.
That package states the ruling in full.

# Why there are no handlers here

The bargain above — you keep your service layer, your handlers and your proto —
is not this package's alone. It is where the module draws the line between what it
stores and what it serves, and the module README states it once, under "Stores and
Transports", along with the few components on the other side of it and the reason
each is there.
*/
package billing

//go:generate go run ./internal/queriesgen
