CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    data         BLOB,
    created_at   DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    expires_at   DATETIME NOT NULL,
    version      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expires_at_idx
    ON sessions (expires_at);

