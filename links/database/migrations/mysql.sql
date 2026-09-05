-- MySQL has no CREATE INDEX IF NOT EXISTS, so the index is declared inline.
-- See postgres.sql for what it serves.
--
-- The TEXT columns are VARCHAR here because the key is indexed and MySQL cannot
-- index a TEXT column without a prefix length. id is VARCHAR(255) rather than
-- the 64 characters a hex SHA-256 occupies, so that a deployment configuring a
-- wider hasher does not need a migration to store its output.
--
-- No convention triple, and that is deliberate. An action link is minted,
-- resolved once, and collected; archived_at would keep rows nothing can ever
-- read, and last_updated_at would be a second copy of resolved_at. What
-- reclaims the table is the sweeper, on purge_after.
--
-- id is the digest of the token and never the token. The token is the whole
-- credential — whoever holds it can redeem the link — so a database copy is a
-- queue of account takeovers if this column holds the raw value. It is the
-- primary key rather than a column beside a surrogate id: a link has exactly
-- one name. See postgres.sql for why there is no scope column.
--
-- action and subject are what the link is bound to. subject carries no
-- REFERENCES, so an application keeping its users somewhere this table cannot
-- name still gets to use action links.
--
-- state is links.State: 1 active, 2 redeemed, 3 revoked. resolved_at is NULL
-- for exactly the active rows, and it is what the resolution guards on.
CREATE TABLE IF NOT EXISTS {{PREFIX}}action_links (
    id          VARCHAR(255) NOT NULL PRIMARY KEY,
    action      VARCHAR(255) NOT NULL,
    subject     VARCHAR(255) NOT NULL,
    metadata    BLOB,
    state       INTEGER      NOT NULL,
    version     INTEGER      NOT NULL,
    created_at  DATETIME(6)  NOT NULL,
    expires_at  DATETIME(6)  NOT NULL,
    resolved_at DATETIME(6),
    purge_after DATETIME(6)  NOT NULL,

    KEY {{PREFIX}}action_links_purge_after_idx (purge_after)
);
