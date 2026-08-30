-- scope is whose billing this is: a reseller, a region, a brand, or — as the
-- empty string — nobody. Every read of every table here filters on it.
--
-- It has no default. The empty string is a scope, tenancy.Global(), and a
-- column that supplied it for a write which did not name one would hand out the
-- global scope to whoever forgot the column — the mistake tenancy.Scope exists
-- to make unspellable in Go. NOT NULL with nothing to fall back on makes that
-- write fail instead. See the tenancy package.

-- What a deployment sells.
--
-- The catalog is scope-wide and carries no account: a product is a thing on
-- offer, and who bought it is the subscription or the purchase. That is why
-- this is the one table here with no belongs_to_account.
--
-- amount_cents is the price in the currency's minor unit, and it is BIGINT
-- rather than INTEGER because a signed 32-bit count of cents runs out at about
-- twenty-one million dollars — which an annual enterprise contract reaches, and
-- which a zero-decimal currency reaches sooner, since amount_cents holds whole
-- yen. currency is the ISO 4217 code the amount is denominated in, stored beside
-- it rather than assumed, because an amount without its currency is a number
-- nobody can charge.
--
-- billing_interval_months is NULL for a one-time product and the recurrence for
-- a subscription one. It is nullable rather than zero-defaulted because zero
-- months is not a billing interval and a column that could hold it would let a
-- recurring product exist that nothing knows when to bill.
--
-- external_product_id is the provider's identifier for the same product, and it
-- is nullable on purpose — see the unique index below.
CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_products (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    name                    TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    kind                    TEXT NOT NULL,
    amount_cents            BIGINT NOT NULL,
    currency                TEXT NOT NULL,
    billing_interval_months BIGINT,
    external_product_id     TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at         TIMESTAMPTZ,
    archived_at             TIMESTAMPTZ
);

-- Serves the catalog page: one scope, live rows, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_products_scope_idx
    ON {{PREFIX}}billing_products (scope, id)
    WHERE archived_at IS NULL;

-- One product per provider-side product, per scope — and NULL repeats freely.
--
-- That last part is the whole reason the column is nullable rather than
-- NOT NULL DEFAULT '', which is this module's habit for text. A free tier and a
-- comped plan are products a deployment sells and never mirrors to a payment
-- provider, so "no provider-side product" is a genuine absence rather than a
-- value; under the empty string the second such product would collide with the
-- first, and the fix would be dropping the uniqueness that keeps two catalog
-- rows from pointing at one Stripe product. All three engines treat NULLs in a
-- unique index as distinct, so this spelling means the same thing on each.
--
-- It is not partial. The uniqueness covers archived rows because an archived
-- product still points at the provider's, and freeing the key on archive is how
-- a second row ends up claiming a product the first one is still reconciled
-- against.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_products_external_uniq
    ON {{PREFIX}}billing_products (scope, external_product_id);

-- A recurring agreement: one account, one product, for as long as it is paid.
--
-- current_period_start and current_period_end are the window the provider says
-- is currently paid for, and both are NOT NULL because a subscription with no
-- period is a subscription nothing can decide the standing of. They are the
-- provider's dates rather than this schema's, restated here so that deciding
-- whether an account is entitled is a read of one row instead of a call to
-- Stripe on a request path — see the entitlements package.
--
-- status is the provider's word for where the agreement stands, stored as it
-- was reported. What it means — which of these values leaves an account
-- entitled — is deliberately not encoded here: that is policy, it differs
-- between deployments that sell the same thing, and capitalism's documentation
-- says where it lives. This column is the fact; the reading is the consumer's.
--
-- There is deliberately no status_changed_at, which waitlist_signups carries and
-- this table does not. A signup's reminder is scheduled off the moment it
-- moved, and nothing else in that table says when to look at it again; here
-- current_period_end is that column, and the events that move the status carry
-- their own timestamps from the provider that sent them.
CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_subscriptions (
    id                       TEXT PRIMARY KEY,
    scope                    TEXT NOT NULL,
    belongs_to_account       TEXT NOT NULL,
    product_id               TEXT NOT NULL REFERENCES {{PREFIX}}billing_products (id),
    external_subscription_id TEXT,
    status                   TEXT NOT NULL,
    current_period_start     TIMESTAMPTZ NOT NULL,
    current_period_end       TIMESTAMPTZ NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at          TIMESTAMPTZ,
    archived_at              TIMESTAMPTZ
);

-- Serves an account's subscription history, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_subscriptions_account_idx
    ON {{PREFIX}}billing_subscriptions (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

-- Serves the read every entitlement check ultimately makes: the account's
-- subscriptions whose paid period covers this instant.
CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_subscriptions_current_idx
    ON {{PREFIX}}billing_subscriptions (scope, belongs_to_account, current_period_end, id)
    WHERE archived_at IS NULL;

-- One subscription per provider-side subscription, per scope, with NULL
-- repeating freely — the same decision the products index documents, for the
-- same reason. A subscription granted by hand, which is what grandfathering
-- somebody looks like, has no provider-side counterpart at all.
--
-- This index is also what makes a redelivered subscription webhook safe: the
-- second insert collides rather than opening a second agreement for one paying
-- customer.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_subscriptions_external_uniq
    ON {{PREFIX}}billing_subscriptions (scope, external_subscription_id);

-- A one-time purchase: bought once, owned afterwards.
--
-- completed_at is NULL until the payment behind it succeeds, which is the whole
-- lifecycle this table has. A purchase row is written when the attempt starts,
-- so that the transaction recording the attempt has something of ours to point
-- at, and it is completed when the provider says the money moved.
--
-- amount_cents and currency are restated here rather than read through
-- product_id, because a price is a fact about the moment of sale. Repricing a
-- product must not rewrite what somebody already paid, and a read that joined to
-- find the amount would do exactly that.
CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_purchases (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    belongs_to_account      TEXT NOT NULL,
    product_id              TEXT NOT NULL REFERENCES {{PREFIX}}billing_products (id),
    external_transaction_id TEXT,
    amount_cents            BIGINT NOT NULL,
    currency                TEXT NOT NULL,
    completed_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at         TIMESTAMPTZ,
    archived_at             TIMESTAMPTZ
);

-- Serves an account's purchase history, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_purchases_account_idx
    ON {{PREFIX}}billing_purchases (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

-- One purchase per provider-side transaction, per scope, NULL repeating freely.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_purchases_external_uniq
    ON {{PREFIX}}billing_purchases (scope, external_transaction_id);

-- What a payment attempt left behind.
--
-- subscription_id and purchase_id are both nullable and at most one is set: a
-- transaction is either a subscription's renewal or a purchase's payment, and a
-- transaction that is neither is the refund of something no longer here. They
-- are two columns rather than one polymorphic pair for the reason the tenancy
-- doctrine gives for the scope — a column spelling "subscription:abc123" carries
-- two facts and can be indexed as neither — and because each has a real foreign
-- key this way.
--
-- amount_cents and currency are the attempt's own, restated for the reason
-- billing_purchases restates them: a partial refund is a transaction whose
-- amount is not the subscription's price, and a ledger that had to join to find
-- an amount could not hold one.
CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_transactions (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    belongs_to_account      TEXT NOT NULL,
    subscription_id         TEXT REFERENCES {{PREFIX}}billing_subscriptions (id),
    purchase_id             TEXT REFERENCES {{PREFIX}}billing_purchases (id),
    external_transaction_id TEXT,
    status                  TEXT NOT NULL,
    amount_cents            BIGINT NOT NULL,
    currency                TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at         TIMESTAMPTZ,
    archived_at             TIMESTAMPTZ
);

-- Serves an account's ledger, walked by id, which is also the order the
-- attempts were made in.
CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_transactions_account_idx
    ON {{PREFIX}}billing_transactions (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

-- One row per provider-side transaction, per scope, NULL repeating freely.
--
-- This is the index that makes the ledger safe to write from a webhook. Payment
-- providers redeliver, and a ledger that recorded the same charge twice is a
-- number somebody reconciles by hand; here the second insert collides and the
-- store reports it as the replay it is. It covers archived rows for the same
-- reason the others do.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_transactions_external_uniq
    ON {{PREFIX}}billing_transactions (scope, external_transaction_id);
