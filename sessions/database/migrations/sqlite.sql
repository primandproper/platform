-- No convention triple, and that is deliberate. A sweeper deletes expired rows
-- outright to keep this table sized by live sessions rather than by every
-- session ever opened, so archived_at would either do nothing or defeat the
-- sweep. last_seen_at, not last_updated_at, is the column a read moves, and it
-- is a liveness signal the store's policy compares against rather than a record
-- of the row's last mutation.
--
-- See postgres.sql for what scope, principal and the four device columns are,
-- and for why the first two carry no default.
CREATE TABLE IF NOT EXISTS {{PREFIX}}sessions (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL,
    principal    TEXT NOT NULL,
    data         BLOB,
    device_name  TEXT NOT NULL,
    ip_address   TEXT NOT NULL,
    user_agent   TEXT NOT NULL,
    login_method TEXT NOT NULL,
    created_at   DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    expires_at   DATETIME NOT NULL,
    version      INTEGER NOT NULL
);

-- Serves the sweeper. See postgres.sql for what expires_at is and is not used
-- for.
CREATE INDEX IF NOT EXISTS {{PREFIX}}sessions_expires_at_idx
    ON {{PREFIX}}sessions (expires_at);

-- Serves the enumeration and the two bulk revocations. See postgres.sql.
CREATE INDEX IF NOT EXISTS {{PREFIX}}sessions_principal_idx
    ON {{PREFIX}}sessions (scope, principal, created_at);
