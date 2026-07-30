CREATE TABLE IF NOT EXISTS {{TABLE}} (
    id            TEXT PRIMARY KEY,
    topic         TEXT NOT NULL,
    partition_key TEXT NOT NULL DEFAULT '',
    payload       BLOB NOT NULL,
    created_at    DATETIME NOT NULL,
    next_attempt  DATETIME NOT NULL,
    claimed_until DATETIME,
    published_at  DATETIME,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT,
    quarantined   BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS {{TABLE}}_claim_idx
    ON {{TABLE}} (next_attempt, created_at, id)
    WHERE published_at IS NULL AND quarantined = FALSE;

CREATE INDEX IF NOT EXISTS {{TABLE}}_ordering_idx
    ON {{TABLE}} (partition_key, created_at, id)
    WHERE published_at IS NULL AND quarantined = FALSE;

CREATE INDEX IF NOT EXISTS {{TABLE}}_reap_idx
    ON {{TABLE}} (published_at)
    WHERE published_at IS NOT NULL;
