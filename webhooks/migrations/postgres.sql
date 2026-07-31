CREATE TABLE IF NOT EXISTS {{PREFIX}}_endpoints (
    id              TEXT PRIMARY KEY,
    url             TEXT NOT NULL,
    content_type    TEXT NOT NULL,
    secret_current  BYTEA NOT NULL,
    secret_previous BYTEA,
    headers         BYTEA NOT NULL,
    disabled        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}_endpoints_live_idx
    ON {{PREFIX}}_endpoints (created_at, id)
    WHERE archived_at IS NULL;

-- Subscriptions are a join table rather than an array column on the endpoint so
-- that "who wants this event" is an index lookup instead of a scan over every
-- registered endpoint. Dispatch runs that query inside the caller's
-- transaction, so its cost is paid by the request that emitted the event.
CREATE TABLE IF NOT EXISTS {{PREFIX}}_subscriptions (
    endpoint_id TEXT NOT NULL REFERENCES {{PREFIX}}_endpoints (id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    PRIMARY KEY (endpoint_id, event_type)
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}_subscriptions_event_idx
    ON {{PREFIX}}_subscriptions (event_type, endpoint_id);

-- The delivery holds the payload once, however many subscribers it fans out to.
CREATE TABLE IF NOT EXISTS {{PREFIX}}_deliveries (
    id           TEXT PRIMARY KEY,
    event_type   TEXT NOT NULL,
    payload      BYTEA NOT NULL,
    ordering_key TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL
);

-- One dispatch per (delivery, endpoint): the row the worker claims, retries,
-- and eventually gives up on. Per-endpoint attempt state lives here and nowhere
-- else, which is what lets four subscribers succeed on the first attempt while
-- a fifth is still backing off.
CREATE TABLE IF NOT EXISTS {{PREFIX}}_dispatches (
    id            TEXT PRIMARY KEY,
    delivery_id   TEXT NOT NULL REFERENCES {{PREFIX}}_deliveries (id) ON DELETE CASCADE,
    endpoint_id   TEXT NOT NULL,
    ordering_key  TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL,
    next_attempt  TIMESTAMPTZ NOT NULL,
    claimed_until TIMESTAMPTZ,
    delivered_at  TIMESTAMPTZ,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    dead          BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (delivery_id, endpoint_id)
);

-- The claim predicate only ever looks at undelivered, live rows, so the index
-- serving it excludes everything else. Without the partial clause this index
-- grows with total history rather than with backlog, and the worker slows down
-- as the table fills.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_dispatches_claim_idx
    ON {{PREFIX}}_dispatches (next_attempt, created_at, id)
    WHERE delivered_at IS NULL AND dead = FALSE;

-- Serves the NOT EXISTS enforcing per-key ordering. The leading column is
-- endpoint_id, not ordering_key: ordering is per (endpoint, key), so that a
-- subscriber which is timing out delays only its own queue for that key and
-- never another subscriber's. id is in the key because the predicate orders on
-- (created_at, id) — dispatches fanned out from one delivery share a timestamp
-- and are separated only by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_dispatches_ordering_idx
    ON {{PREFIX}}_dispatches (endpoint_id, ordering_key, created_at, id)
    WHERE delivered_at IS NULL AND dead = FALSE;

-- Serves Requeue, which names a (delivery, endpoint) pair.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_dispatches_replay_idx
    ON {{PREFIX}}_dispatches (delivery_id, endpoint_id);

-- Serves the reaper.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_dispatches_reap_idx
    ON {{PREFIX}}_dispatches (delivered_at)
    WHERE delivered_at IS NOT NULL;

-- The delivery log. Append-only, and deliberately not constrained by a foreign
-- key to the endpoint: an endpoint can be archived, and "what did we send them"
-- is asked most often after someone has been removed.
CREATE TABLE IF NOT EXISTS {{PREFIX}}_attempts (
    id            TEXT PRIMARY KEY,
    delivery_id   TEXT NOT NULL,
    endpoint_id   TEXT NOT NULL,
    attempt_count INTEGER NOT NULL,
    status_code   INTEGER NOT NULL DEFAULT 0,
    error         TEXT,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    attempted_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}_attempts_delivery_idx
    ON {{PREFIX}}_attempts (delivery_id, attempted_at, id);

CREATE INDEX IF NOT EXISTS {{PREFIX}}_attempts_endpoint_idx
    ON {{PREFIX}}_attempts (endpoint_id, attempted_at, id);
