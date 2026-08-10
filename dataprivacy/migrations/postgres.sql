-- One table. A privacy request is a single row with a status, and the prior art
-- that split it across a request table and a disclosure table gained nothing
-- from the join except the possibility of the two disagreeing about whether an
-- artifact still existed.
CREATE TABLE IF NOT EXISTS {{PREFIX}}dataprivacy_requests (
    id             TEXT PRIMARY KEY,
    request_type   TEXT NOT NULL,
    status         TEXT NOT NULL,
    operation_id   TEXT NOT NULL DEFAULT '',
    subject_id     TEXT NOT NULL,
    subject_type   TEXT NOT NULL DEFAULT '',
    subject_scope  TEXT NOT NULL DEFAULT '',
    requested_at   TIMESTAMPTZ NOT NULL,
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

-- "What has been asked in this person's name." Leading with the subject rather
-- than the time is what makes List a range scan instead of a filter over every
-- request the system has ever served. requested_at descends because a subject
-- listing their requests wants the most recent first, and a DESC index is what
-- keeps that from being a sort.
CREATE INDEX IF NOT EXISTS {{PREFIX}}dataprivacy_requests_subject_idx
    ON {{PREFIX}}dataprivacy_requests (subject_id, subject_scope, requested_at DESC, id DESC);

-- Serves the artifact expiry sweep. Partial on the one status that can hold a
-- live artifact: an expired or failed request has nothing in the bucket, and a
-- sweep that scanned them would do more work every day it ran.
CREATE INDEX IF NOT EXISTS {{PREFIX}}dataprivacy_requests_expiry_idx
    ON {{PREFIX}}dataprivacy_requests (expires_at)
    WHERE status = 'completed' AND artifact_ref <> '';

-- Serves the confirmation-window lapse sweep.
CREATE INDEX IF NOT EXISTS {{PREFIX}}dataprivacy_requests_confirmation_idx
    ON {{PREFIX}}dataprivacy_requests (expires_at)
    WHERE status = 'awaiting_confirmation';

-- Serves the overdue gauge and the retention reap, both of which ask a question
-- about status and a timestamp and nothing else.
CREATE INDEX IF NOT EXISTS {{PREFIX}}dataprivacy_requests_status_due_idx
    ON {{PREFIX}}dataprivacy_requests (status, due_at);

CREATE INDEX IF NOT EXISTS {{PREFIX}}dataprivacy_requests_reap_idx
    ON {{PREFIX}}dataprivacy_requests (completed_at)
    WHERE completed_at IS NOT NULL;
