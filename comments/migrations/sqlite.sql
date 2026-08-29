-- One table: something somebody said, about something the application owns. The
-- Postgres schema carries the long form of why.
CREATE TABLE IF NOT EXISTS {{PREFIX}}comments (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    target_type     TEXT NOT NULL,
    target_id       TEXT NOT NULL,
    parent_id       TEXT NOT NULL DEFAULT '',
    author          TEXT NOT NULL,
    body            TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}comments_target_idx
    ON {{PREFIX}}comments (scope, target_type, target_id, parent_id, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS {{PREFIX}}comments_target_type_idx
    ON {{PREFIX}}comments (scope, target_type, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS {{PREFIX}}comments_author_idx
    ON {{PREFIX}}comments (scope, author, id)
    WHERE archived_at IS NULL;
