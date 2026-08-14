-- MySQL has no CREATE INDEX IF NOT EXISTS, so the sweeper's index is declared
-- inline. See postgres.sql for what expires_at is read by.
--
-- challenge is VARCHAR(255) rather than TEXT because MySQL cannot index a TEXT
-- column without a prefix length, and the primary key is the whole point of
-- this table. A challenge is the base64url rendering of at least 16 random
-- bytes — 43 characters for the 32 the library mints — so the declared width is
-- room to grow rather than a limit anyone will reach.
CREATE TABLE IF NOT EXISTS {{PREFIX}}webauthn_sessions (
    challenge    VARCHAR(255) NOT NULL PRIMARY KEY,
    session_data LONGBLOB     NOT NULL,
    expires_at   DATETIME(6)  NOT NULL,

    KEY {{PREFIX}}webauthn_sessions_expires_at_idx (expires_at)
);
