-- scope is whose endpoint it is: an account, an organization, a workspace, or —
-- as the empty string — nobody. Every read of this table filters on it, and the
-- default is what makes it adoptable: rows written before the column existed
-- become global rows, and an application whose events are global keeps behaving
-- as it did. See the tenancy package.
CREATE TABLE IF NOT EXISTS {{PREFIX}}webhooks_endpoints (
    id              VARCHAR(64) NOT NULL PRIMARY KEY,
    scope           VARCHAR(255) NOT NULL DEFAULT '',
    url             TEXT NOT NULL,
    content_type    VARCHAR(255) NOT NULL,
    secret_current  VARBINARY(512) NOT NULL,
    secret_previous VARBINARY(512),
    headers         BLOB NOT NULL,
    disabled        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      DATETIME(6) NOT NULL,
    updated_at      DATETIME(6),
    archived_at     DATETIME(6)
);

CREATE INDEX {{PREFIX}}webhooks_endpoints_live_idx
    ON {{PREFIX}}webhooks_endpoints (archived_at, created_at, id);

-- Serves the registry read, which is always one scope's page ordered by id, and
-- is available to the fan-out join's scope predicate. Leading with scope is what
-- keeps one tenant's page from walking every other tenant's rows.
CREATE INDEX {{PREFIX}}webhooks_endpoints_scope_idx
    ON {{PREFIX}}webhooks_endpoints (scope, archived_at, id);

-- Subscriptions are a join table rather than an array column on the endpoint so
-- that "who wants this event" is an index lookup instead of a scan over every
-- registered endpoint. Dispatch runs that query inside the caller's
-- transaction, so its cost is paid by the request that emitted the event.
CREATE TABLE IF NOT EXISTS {{PREFIX}}webhooks_subscriptions (
    endpoint_id VARCHAR(64) NOT NULL,
    event_type  VARCHAR(255) NOT NULL,
    PRIMARY KEY (endpoint_id, event_type),
    CONSTRAINT {{PREFIX}}webhooks_subscriptions_endpoint_fk
        FOREIGN KEY (endpoint_id) REFERENCES {{PREFIX}}webhooks_endpoints (id) ON DELETE CASCADE
);

CREATE INDEX {{PREFIX}}webhooks_subscriptions_event_idx
    ON {{PREFIX}}webhooks_subscriptions (event_type, endpoint_id);

-- The delivery holds the payload once, however many subscribers it fans out to.
--
-- Its scope is whose event it was. It bounds the fan-out that produced the
-- dispatches below, and it is what the delivery log is read through — an attempt
-- carries no scope of its own, because the delivery it describes already has one.
CREATE TABLE IF NOT EXISTS {{PREFIX}}webhooks_deliveries (
    id           VARCHAR(64) NOT NULL PRIMARY KEY,
    scope        VARCHAR(255) NOT NULL DEFAULT '',
    event_type   VARCHAR(255) NOT NULL,
    payload      LONGBLOB NOT NULL,
    ordering_key VARCHAR(255) NOT NULL DEFAULT '',
    created_at   DATETIME(6) NOT NULL
);

-- One dispatch per (delivery, endpoint): the row the worker claims, retries,
-- and eventually gives up on. Per-endpoint attempt state lives here and nowhere
-- else, which is what lets four subscribers succeed on the first attempt while
-- a fifth is still backing off.
CREATE TABLE IF NOT EXISTS {{PREFIX}}webhooks_dispatches (
    id            VARCHAR(64) NOT NULL PRIMARY KEY,
    delivery_id   VARCHAR(64) NOT NULL,
    endpoint_id   VARCHAR(64) NOT NULL,
    ordering_key  VARCHAR(255) NOT NULL DEFAULT '',
    created_at    DATETIME(6) NOT NULL,
    next_attempt  DATETIME(6) NOT NULL,
    claimed_until DATETIME(6),
    delivered_at  DATETIME(6),
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT,
    dead          BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE KEY {{PREFIX}}webhooks_dispatches_pair_uniq (delivery_id, endpoint_id),
    CONSTRAINT {{PREFIX}}webhooks_dispatches_delivery_fk
        FOREIGN KEY (delivery_id) REFERENCES {{PREFIX}}webhooks_deliveries (id) ON DELETE CASCADE
);

-- MySQL has no partial indexes, so unlike the Postgres schema these cover the
-- whole table and the predicate columns lead. The claim and ordering queries
-- filter on delivered_at/dead first, so putting them in front keeps the index
-- selective for the same queries the partial clause serves elsewhere.
CREATE INDEX {{PREFIX}}webhooks_dispatches_claim_idx
    ON {{PREFIX}}webhooks_dispatches (delivered_at, dead, next_attempt, created_at, id);

-- Serves the NOT EXISTS enforcing per-key ordering. The leading column is
-- endpoint_id, not ordering_key: ordering is per (endpoint, key), so that a
-- subscriber which is timing out delays only its own queue for that key and
-- never another subscriber's. id is in the key because the predicate orders on
-- (created_at, id) — dispatches fanned out from one delivery share a timestamp
-- and are separated only by id.
CREATE INDEX {{PREFIX}}webhooks_dispatches_ordering_idx
    ON {{PREFIX}}webhooks_dispatches (endpoint_id, ordering_key, delivered_at, dead, created_at, id);

-- Serves the reaper.
CREATE INDEX {{PREFIX}}webhooks_dispatches_reap_idx
    ON {{PREFIX}}webhooks_dispatches (delivered_at, id);

-- The delivery log. Append-only, and deliberately not constrained by a foreign
-- key to the endpoint: an endpoint can be archived, and "what did we send them"
-- is asked most often after someone has been removed.
CREATE TABLE IF NOT EXISTS {{PREFIX}}webhooks_attempts (
    id            VARCHAR(64) NOT NULL PRIMARY KEY,
    delivery_id   VARCHAR(64) NOT NULL,
    endpoint_id   VARCHAR(64) NOT NULL,
    attempt_count INT NOT NULL,
    status_code   INT NOT NULL DEFAULT 0,
    error         TEXT,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    attempted_at  DATETIME(6) NOT NULL
);

CREATE INDEX {{PREFIX}}webhooks_attempts_delivery_idx
    ON {{PREFIX}}webhooks_attempts (delivery_id, attempted_at, id);

CREATE INDEX {{PREFIX}}webhooks_attempts_endpoint_idx
    ON {{PREFIX}}webhooks_attempts (endpoint_id, attempted_at, id);
