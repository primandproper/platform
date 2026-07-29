CREATE TABLE IF NOT EXISTS {{TABLE}} (
    id            TEXT PRIMARY KEY,
    topic         TEXT NOT NULL,
    partition_key TEXT NOT NULL DEFAULT '',
    payload       BYTEA NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL,
    next_attempt  TIMESTAMPTZ NOT NULL,
    claimed_until TIMESTAMPTZ,
    published_at  TIMESTAMPTZ,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    quarantined   BOOLEAN NOT NULL DEFAULT FALSE
);

-- The claim predicate only ever looks at unpublished, unquarantined rows, so
-- the index that serves it excludes everything else. Without the partial
-- clause this index grows with total history rather than with backlog, and the
-- relay slows down as the table fills.
CREATE INDEX IF NOT EXISTS {{TABLE}}_claim_idx
    ON {{TABLE}} (next_attempt, created_at, id)
    WHERE published_at IS NULL AND quarantined = FALSE;

-- Serves the NOT EXISTS that enforces per-key ordering. id is part of the key
-- because the predicate orders on (created_at, id), not created_at alone —
-- messages enqueued in one call share a timestamp and are separated only by id.
CREATE INDEX IF NOT EXISTS {{TABLE}}_ordering_idx
    ON {{TABLE}} (partition_key, created_at, id)
    WHERE published_at IS NULL AND quarantined = FALSE;

-- Serves the reaper.
CREATE INDEX IF NOT EXISTS {{TABLE}}_reap_idx
    ON {{TABLE}} (published_at)
    WHERE published_at IS NOT NULL;
