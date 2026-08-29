CREATE TABLE IF NOT EXISTS comments (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    target_type     TEXT NOT NULL,
    target_id       TEXT NOT NULL,
    parent_id       TEXT NOT NULL DEFAULT '',
    author          TEXT NOT NULL,
    body            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS comments_target_idx
    ON comments (scope, target_type, target_id, parent_id, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS comments_target_type_idx
    ON comments (scope, target_type, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS comments_author_idx
    ON comments (scope, author, id)
    WHERE archived_at IS NULL;

