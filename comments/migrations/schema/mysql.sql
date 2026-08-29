CREATE TABLE IF NOT EXISTS comments (
    id              VARCHAR(64) NOT NULL PRIMARY KEY,
    scope           VARCHAR(255) NOT NULL,
    target_type     VARCHAR(255) NOT NULL,
    target_id       VARCHAR(64) NOT NULL,
    parent_id       VARCHAR(64) NOT NULL DEFAULT '',
    author          VARCHAR(64) NOT NULL,
    body            TEXT NOT NULL,
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at DATETIME(6),
    archived_at     DATETIME(6)
);

CREATE INDEX comments_target_idx
    ON comments (scope, archived_at, target_type, target_id, parent_id, id);

CREATE INDEX comments_target_type_idx
    ON comments (scope, archived_at, target_type, id);

CREATE INDEX comments_author_idx
    ON comments (scope, archived_at, author, id);

