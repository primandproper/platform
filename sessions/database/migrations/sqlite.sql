-- No convention triple, and that is deliberate. A sweeper deletes expired rows
-- outright to keep this table sized by live sessions rather than by every
-- session ever opened, so archived_at would either do nothing or defeat the
-- sweep. last_seen_at, not last_updated_at, is the column a read moves, and it
-- is a liveness signal the store's policy compares against rather than a record
-- of the row's last mutation.
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
