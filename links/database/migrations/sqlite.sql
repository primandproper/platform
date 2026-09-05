-- No convention triple, and that is deliberate. An action link is minted,
-- resolved once, and collected; archived_at would keep rows nothing can ever
-- read, and last_updated_at would be a second copy of resolved_at. What
-- reclaims the table is the sweeper, on purge_after.
--
-- id is the digest of the token and never the token, and it is the primary key
-- rather than a column beside a surrogate id. See postgres.sql for why, for
-- what the index serves, and for why there is no scope column.
--
-- action and subject are what the link is bound to. subject carries no
-- REFERENCES, so an application keeping its users somewhere this table cannot
-- name still gets to use action links.
--
-- state is links.State: 1 active, 2 redeemed, 3 revoked. resolved_at is NULL
-- for exactly the active rows, and it is what the resolution guards on.
CREATE TABLE IF NOT EXISTS {{PREFIX}}action_links (
    id          TEXT PRIMARY KEY,
    action      TEXT NOT NULL,
    subject     TEXT NOT NULL,
    metadata    BLOB,
    state       INTEGER NOT NULL,
    version     INTEGER NOT NULL,
    created_at  DATETIME NOT NULL,
    expires_at  DATETIME NOT NULL,
    resolved_at DATETIME,
    purge_after DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}action_links_purge_after_idx
    ON {{PREFIX}}action_links (purge_after);
