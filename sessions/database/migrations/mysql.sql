-- MySQL has no CREATE INDEX IF NOT EXISTS, so the sweeper's index is declared
-- inline. See postgres.sql for what expires_at is and is not used for.
--
-- id is VARCHAR(255) rather than TEXT because MySQL cannot index a TEXT column
-- without a prefix length, and the primary key is the whole point of this
-- table. A base64url identifier of DefaultIDByteLength bytes is 43 characters,
-- so the declared width is room to grow rather than a limit anyone will reach.
--
-- No convention triple, and that is deliberate. A sweeper deletes expired rows
-- outright to keep this table sized by live sessions rather than by every
-- session ever opened, so archived_at would either do nothing or defeat the
-- sweep. last_seen_at, not last_updated_at, is the column a read moves, and it
-- is a liveness signal the store's policy compares against rather than a record
-- of the row's last mutation.
CREATE TABLE IF NOT EXISTS {{PREFIX}}sessions (
    id           VARCHAR(255) NOT NULL PRIMARY KEY,
    data         LONGBLOB     NULL,
    created_at   DATETIME(6)  NOT NULL,
    last_seen_at DATETIME(6)  NOT NULL,
    expires_at   DATETIME(6)  NOT NULL,
    version      INT          NOT NULL,

    KEY {{PREFIX}}sessions_expires_at_idx (expires_at)
);
