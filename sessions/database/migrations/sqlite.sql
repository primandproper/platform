CREATE TABLE IF NOT EXISTS {{PREFIX}}sessions (
    id           TEXT PRIMARY KEY,
    data         BLOB,
    created_at   DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    expires_at   DATETIME NOT NULL,
    version      INTEGER NOT NULL
);

-- Serves the sweeper. See postgres.sql for what expires_at is and is not used
-- for.
CREATE INDEX IF NOT EXISTS {{PREFIX}}sessions_expires_at_idx
    ON {{PREFIX}}sessions (expires_at);
