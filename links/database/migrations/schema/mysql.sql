CREATE TABLE IF NOT EXISTS action_links (
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
    KEY action_links_purge_after_idx (purge_after)
);

