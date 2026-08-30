CREATE TABLE IF NOT EXISTS billing_products (
    id                      VARCHAR(64) NOT NULL PRIMARY KEY,
    scope                   VARCHAR(255) NOT NULL,
    name                    VARCHAR(255) NOT NULL,
    description             VARCHAR(1024) NOT NULL DEFAULT '',
    kind                    VARCHAR(32) NOT NULL,
    amount_cents            BIGINT NOT NULL,
    currency                VARCHAR(8) NOT NULL,
    billing_interval_months BIGINT,
    external_product_id     VARCHAR(255),
    created_at              DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at         DATETIME(6),
    archived_at             DATETIME(6),
    UNIQUE KEY billing_products_external_uniq (scope, external_product_id)
);

CREATE INDEX billing_products_scope_idx
    ON billing_products (scope, archived_at, id);

CREATE TABLE IF NOT EXISTS billing_subscriptions (
    id                       VARCHAR(64) NOT NULL PRIMARY KEY,
    scope                    VARCHAR(255) NOT NULL,
    belongs_to_account       VARCHAR(64) NOT NULL,
    product_id               VARCHAR(64) NOT NULL,
    external_subscription_id VARCHAR(255),
    status                   VARCHAR(32) NOT NULL,
    current_period_start     DATETIME(6) NOT NULL,
    current_period_end       DATETIME(6) NOT NULL,
    created_at               DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at          DATETIME(6),
    archived_at              DATETIME(6),
    UNIQUE KEY billing_subscriptions_external_uniq (scope, external_subscription_id),
    CONSTRAINT billing_subscriptions_product_fk
        FOREIGN KEY (product_id) REFERENCES billing_products (id)
);

CREATE INDEX billing_subscriptions_account_idx
    ON billing_subscriptions (scope, belongs_to_account, archived_at, id);

CREATE INDEX billing_subscriptions_current_idx
    ON billing_subscriptions (scope, belongs_to_account, archived_at, current_period_end, id);

CREATE TABLE IF NOT EXISTS billing_purchases (
    id                      VARCHAR(64) NOT NULL PRIMARY KEY,
    scope                   VARCHAR(255) NOT NULL,
    belongs_to_account      VARCHAR(64) NOT NULL,
    product_id              VARCHAR(64) NOT NULL,
    external_transaction_id VARCHAR(255),
    amount_cents            BIGINT NOT NULL,
    currency                VARCHAR(8) NOT NULL,
    completed_at            DATETIME(6),
    created_at              DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at         DATETIME(6),
    archived_at             DATETIME(6),
    UNIQUE KEY billing_purchases_external_uniq (scope, external_transaction_id),
    CONSTRAINT billing_purchases_product_fk
        FOREIGN KEY (product_id) REFERENCES billing_products (id)
);

CREATE INDEX billing_purchases_account_idx
    ON billing_purchases (scope, belongs_to_account, archived_at, id);

CREATE TABLE IF NOT EXISTS billing_transactions (
    id                      VARCHAR(64) NOT NULL PRIMARY KEY,
    scope                   VARCHAR(255) NOT NULL,
    belongs_to_account      VARCHAR(64) NOT NULL,
    subscription_id         VARCHAR(64),
    purchase_id             VARCHAR(64),
    external_transaction_id VARCHAR(255),
    status                  VARCHAR(32) NOT NULL,
    amount_cents            BIGINT NOT NULL,
    currency                VARCHAR(8) NOT NULL,
    created_at              DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at         DATETIME(6),
    archived_at             DATETIME(6),
    UNIQUE KEY billing_transactions_external_uniq (scope, external_transaction_id),
    CONSTRAINT billing_transactions_subscription_fk
        FOREIGN KEY (subscription_id) REFERENCES billing_subscriptions (id),
    CONSTRAINT billing_transactions_purchase_fk
        FOREIGN KEY (purchase_id) REFERENCES billing_purchases (id)
);

CREATE INDEX billing_transactions_account_idx
    ON billing_transactions (scope, belongs_to_account, archived_at, id);

