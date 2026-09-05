CREATE TABLE IF NOT EXISTS action_links (
    id          TEXT PRIMARY KEY,
    action      TEXT NOT NULL,
    subject     TEXT NOT NULL,
    metadata    BYTEA,
    state       INTEGER NOT NULL,
    version     INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    purge_after TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS action_links_purge_after_idx
    ON action_links (purge_after);

