-- scope is whose billing this is: a reseller, a region, a brand, or — as the
-- empty string — nobody. Every read of every table here filters on it.
--
-- It has no default. The empty string is a scope, tenancy.Global(), and a
-- column that supplied it for a write which did not name one would hand out the
-- global scope to whoever forgot the column — the mistake tenancy.Scope exists
-- to make unspellable in Go. NOT NULL with nothing to fall back on makes that
-- write fail instead. See the tenancy package.
--
-- The timestamp columns are DATETIME, which SQLite stores as text. That is what
-- makes them filterable: a window comparison over them is lexicographic, and
-- CURRENT_TIMESTAMP writes the UTC `YYYY-MM-DD HH:MM:SS` shape that
-- lexicographic order agrees with. It is also why every time this store binds is
-- normalized to UTC first — see billing/rows.go.
--
-- SQLite's CURRENT_TIMESTAMP is second-granular, so a created_at written here
-- carries no sub-second part. Nothing in this schema compares two rows' creation
-- times against each other, and the cursor walks the id rather than the clock,
-- so the narrowing costs this package nothing.
--
-- See billing/migrations/postgres.sql for what every column and index is for;
-- the reasoning is written once, there.
CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_products (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    name                    TEXT NOT NULL,
    description             TEXT NOT NULL DEFAULT '',
    kind                    TEXT NOT NULL,
    amount_cents            INTEGER NOT NULL,
    currency                TEXT NOT NULL,
    billing_interval_months INTEGER,
    external_product_id     TEXT,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at         DATETIME,
    archived_at             DATETIME
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_products_scope_idx
    ON {{PREFIX}}billing_products (scope, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_products_external_uniq
    ON {{PREFIX}}billing_products (scope, external_product_id);

CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_subscriptions (
    id                       TEXT PRIMARY KEY,
    scope                    TEXT NOT NULL,
    belongs_to_account       TEXT NOT NULL,
    product_id               TEXT NOT NULL REFERENCES {{PREFIX}}billing_products (id),
    external_subscription_id TEXT,
    status                   TEXT NOT NULL,
    current_period_start     DATETIME NOT NULL,
    current_period_end       DATETIME NOT NULL,
    created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at          DATETIME,
    archived_at              DATETIME
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_subscriptions_account_idx
    ON {{PREFIX}}billing_subscriptions (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_subscriptions_current_idx
    ON {{PREFIX}}billing_subscriptions (scope, belongs_to_account, current_period_end, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_subscriptions_external_uniq
    ON {{PREFIX}}billing_subscriptions (scope, external_subscription_id);

CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_purchases (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    belongs_to_account      TEXT NOT NULL,
    product_id              TEXT NOT NULL REFERENCES {{PREFIX}}billing_products (id),
    external_transaction_id TEXT,
    amount_cents            INTEGER NOT NULL,
    currency                TEXT NOT NULL,
    completed_at            DATETIME,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at         DATETIME,
    archived_at             DATETIME
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_purchases_account_idx
    ON {{PREFIX}}billing_purchases (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_purchases_external_uniq
    ON {{PREFIX}}billing_purchases (scope, external_transaction_id);

CREATE TABLE IF NOT EXISTS {{PREFIX}}billing_transactions (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    belongs_to_account      TEXT NOT NULL,
    subscription_id         TEXT REFERENCES {{PREFIX}}billing_subscriptions (id),
    purchase_id             TEXT REFERENCES {{PREFIX}}billing_purchases (id),
    external_transaction_id TEXT,
    status                  TEXT NOT NULL,
    amount_cents            INTEGER NOT NULL,
    currency                TEXT NOT NULL,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at         DATETIME,
    archived_at             DATETIME
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}billing_transactions_account_idx
    ON {{PREFIX}}billing_transactions (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}billing_transactions_external_uniq
    ON {{PREFIX}}billing_transactions (scope, external_transaction_id);
