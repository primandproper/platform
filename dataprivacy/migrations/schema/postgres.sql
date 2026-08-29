CREATE TABLE IF NOT EXISTS dataprivacy_requests (
    id             TEXT PRIMARY KEY,
    request_type   TEXT NOT NULL,
    status         TEXT NOT NULL,
    operation_id   TEXT NOT NULL DEFAULT '',
    subject_id     TEXT NOT NULL,
    subject_type   TEXT NOT NULL DEFAULT '',
    subject_scope  TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ,
    due_at         TIMESTAMPTZ NOT NULL,
    expires_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    artifact_ref   TEXT NOT NULL DEFAULT '',
    artifact_bytes BIGINT NOT NULL DEFAULT 0,
    deleted_rows   BIGINT NOT NULL DEFAULT 0,
    anonymized_rows BIGINT NOT NULL DEFAULT 0,
    failures       BYTEA,
    retained       BYTEA,
    last_error     TEXT,
    key_shredded_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS dataprivacy_requests_subject_idx
    ON dataprivacy_requests (subject_id, subject_scope, created_at DESC, id DESC);

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

