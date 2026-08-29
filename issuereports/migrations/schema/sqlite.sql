CREATE TABLE IF NOT EXISTS issue_reports (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    reporter        TEXT NOT NULL,
    kind            TEXT NOT NULL,
    details         TEXT NOT NULL,
    subject_type    TEXT NOT NULL DEFAULT '',
    subject_id      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL,
    resolution      TEXT NOT NULL DEFAULT '',
    closed_at       DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

CREATE INDEX IF NOT EXISTS issue_reports_scope_idx
    ON issue_reports (scope, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS issue_reports_status_idx
    ON issue_reports (scope, status, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS issue_reports_reporter_idx
    ON issue_reports (scope, reporter, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS issue_reports_subject_idx
    ON issue_reports (scope, subject_type, subject_id, id)
    WHERE archived_at IS NULL;

