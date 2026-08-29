-- MySQL has no CREATE INDEX IF NOT EXISTS, so both indexes are declared inline.
-- See postgres.sql for what expires_at is and is not used for, for what scope,
-- principal and the four device columns are, and for why the first two carry no
-- default.
--
-- id, scope and principal are VARCHAR(255) rather than TEXT for one reason and
-- one reason only: MySQL cannot index a TEXT column without a prefix length,
-- and these three are the whole of this table's indexing — id is the primary
-- key, and the other two lead the enumeration's key. A base64url identifier of
-- DefaultIDByteLength bytes is 43 characters, so the declared width is room to
-- grow rather than a limit anyone will reach. Nothing else here is narrowed,
-- and no column should be narrowed for tidiness.
--
-- The four device columns are in no index and are therefore TEXT, which is what
-- the other two dialects declare them as. A VARCHAR would not be the same
-- column: the create statement is INSERT IGNORE, for its id-conflict semantics,
-- and IGNORE downgrades MySQL's truncation error to a warning — so an over-long
-- value would be silently cut here and stored whole on Postgres and SQLite.
-- These are client self-descriptions, and a user agent runs past 255 characters
-- in the wild, so that is a dialect-dependent difference in what a security
-- page renders rather than a hypothetical one.
--
-- No convention triple, and that is deliberate. A sweeper deletes expired rows
-- outright to keep this table sized by live sessions rather than by every
-- session ever opened, so archived_at would either do nothing or defeat the
-- sweep. last_seen_at, not last_updated_at, is the column a read moves, and it
-- is a liveness signal the store's policy compares against rather than a record
-- of the row's last mutation.
CREATE TABLE IF NOT EXISTS {{PREFIX}}sessions (
    id           VARCHAR(255) NOT NULL PRIMARY KEY,
    scope        VARCHAR(255) NOT NULL,
    principal    VARCHAR(255) NOT NULL,
    data         LONGBLOB     NULL,
    device_name  TEXT         NOT NULL,
    ip_address   TEXT         NOT NULL,
    user_agent   TEXT         NOT NULL,
    login_method TEXT         NOT NULL,
    created_at   DATETIME(6)  NOT NULL,
    last_seen_at DATETIME(6)  NOT NULL,
    expires_at   DATETIME(6)  NOT NULL,
    version      INT          NOT NULL,

    KEY {{PREFIX}}sessions_expires_at_idx (expires_at),
    KEY {{PREFIX}}sessions_principal_idx (scope, principal, created_at)
);
