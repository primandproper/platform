CREATE TABLE IF NOT EXISTS action_links (
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

CREATE INDEX IF NOT EXISTS action_links_purge_after_idx
    ON action_links (purge_after);

