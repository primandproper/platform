CREATE TABLE IF NOT EXISTS dataprivacy_requests (
    id              TEXT PRIMARY KEY,
    request_type    TEXT NOT NULL,
    status          TEXT NOT NULL,
    operation_id    TEXT NOT NULL DEFAULT '',
    subject_id      TEXT NOT NULL,
    subject_type    TEXT NOT NULL DEFAULT '',
    subject_scope   TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME,
    due_at          DATETIME NOT NULL,
    expires_at      DATETIME,
    completed_at    DATETIME,
    artifact_ref    TEXT NOT NULL DEFAULT '',
    artifact_bytes  INTEGER NOT NULL DEFAULT 0,
    deleted_rows    INTEGER NOT NULL DEFAULT 0,
    anonymized_rows INTEGER NOT NULL DEFAULT 0,
    failures        BLOB,
    retained        BLOB,
    last_error      TEXT,
    key_shredded_at DATETIME
);

CREATE INDEX IF NOT EXISTS dataprivacy_requests_subject_idx
    ON dataprivacy_requests (subject_id, subject_scope, created_at, id);

CREATE INDEX IF NOT EXISTS dataprivacy_requests_expiry_idx
    ON dataprivacy_requests (expires_at)
    WHERE status = 'completed' AND artifact_ref <> '';

CREATE INDEX IF NOT EXISTS dataprivacy_requests_confirmation_idx
    ON dataprivacy_requests (expires_at)
    WHERE status = 'awaiting_confirmation';

CREATE INDEX IF NOT EXISTS dataprivacy_requests_status_due_idx
    ON dataprivacy_requests (status, due_at);

CREATE INDEX IF NOT EXISTS dataprivacy_requests_reap_idx
    ON dataprivacy_requests (completed_at)
    WHERE completed_at IS NOT NULL;

