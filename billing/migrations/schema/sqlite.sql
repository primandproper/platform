CREATE TABLE IF NOT EXISTS billing_products (
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

CREATE INDEX IF NOT EXISTS billing_products_scope_idx
    ON billing_products (scope, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS billing_products_external_uniq
    ON billing_products (scope, external_product_id);

CREATE TABLE IF NOT EXISTS billing_subscriptions (
    id                       TEXT PRIMARY KEY,
    scope                    TEXT NOT NULL,
    belongs_to_account       TEXT NOT NULL,
    product_id               TEXT NOT NULL REFERENCES billing_products (id),
    external_subscription_id TEXT,
    status                   TEXT NOT NULL,
    current_period_start     DATETIME NOT NULL,
    current_period_end       DATETIME NOT NULL,
    created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at          DATETIME,
    archived_at              DATETIME
);

CREATE INDEX IF NOT EXISTS billing_subscriptions_account_idx
    ON billing_subscriptions (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS billing_subscriptions_current_idx
    ON billing_subscriptions (scope, belongs_to_account, current_period_end, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS billing_subscriptions_external_uniq
    ON billing_subscriptions (scope, external_subscription_id);

CREATE TABLE IF NOT EXISTS billing_purchases (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    belongs_to_account      TEXT NOT NULL,
    product_id              TEXT NOT NULL REFERENCES billing_products (id),
    external_transaction_id TEXT,
    amount_cents            INTEGER NOT NULL,
    currency                TEXT NOT NULL,
    completed_at            DATETIME,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at         DATETIME,
    archived_at             DATETIME
);

CREATE INDEX IF NOT EXISTS billing_purchases_account_idx
    ON billing_purchases (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS billing_purchases_external_uniq
    ON billing_purchases (scope, external_transaction_id);

CREATE TABLE IF NOT EXISTS billing_transactions (
    id                      TEXT PRIMARY KEY,
    scope                   TEXT NOT NULL,
    belongs_to_account      TEXT NOT NULL,
    subscription_id         TEXT REFERENCES billing_subscriptions (id),
    purchase_id             TEXT REFERENCES billing_purchases (id),
    external_transaction_id TEXT,
    status                  TEXT NOT NULL,
    amount_cents            INTEGER NOT NULL,
    currency                TEXT NOT NULL,
    created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at         DATETIME,
    archived_at             DATETIME
);

CREATE INDEX IF NOT EXISTS billing_transactions_account_idx
    ON billing_transactions (scope, belongs_to_account, id)
    WHERE archived_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS billing_transactions_external_uniq
    ON billing_transactions (scope, external_transaction_id);

