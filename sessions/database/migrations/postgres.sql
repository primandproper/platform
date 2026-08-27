-- No convention triple, and that is deliberate. A sweeper deletes expired rows
-- outright to keep this table sized by live sessions rather than by every
-- session ever opened, so archived_at would either do nothing or defeat the
-- sweep. last_seen_at, not last_updated_at, is the column a read moves, and it
-- is a liveness signal the store's policy compares against rather than a record
-- of the row's last mutation.
CREATE TABLE IF NOT EXISTS {{PREFIX}}sessions (
    id           TEXT PRIMARY KEY,
    data         BYTEA,
    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    version      INTEGER NOT NULL
);

-- Serves the sweeper, which is the only query that reads a row it cannot name.
-- Every other statement in this package is keyed by the primary key.
--
-- expires_at is written by the backend and read by nothing else: whether a
-- session is live is decided from created_at and last_seen_at against the
-- store's policy, so that both backends answer the question the same way. This
-- column exists so that dead rows can be found without scanning, and it is
-- deliberately not part of any read predicate — a clock skew between a writer
-- and a reader would otherwise hide live sessions.
CREATE INDEX IF NOT EXISTS {{PREFIX}}sessions_expires_at_idx
    ON {{PREFIX}}sessions (expires_at);
