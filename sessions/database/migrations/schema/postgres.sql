CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    data         BYTEA,
    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    version      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expires_at_idx
    ON sessions (expires_at);

