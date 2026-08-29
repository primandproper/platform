-- One table. A privacy request is a single row with a status, and the prior art
-- that split it across a request table and a disclosure table gained nothing
-- from the join except the possibility of the two disagreeing about whether an
-- artifact still existed.
CREATE TABLE IF NOT EXISTS {{PREFIX}}dataprivacy_requests (
    id              TEXT PRIMARY KEY,
    request_type    TEXT NOT NULL,
    status          TEXT NOT NULL,
    operation_id    TEXT NOT NULL DEFAULT '',
    subject_id      TEXT NOT NULL,
    subject_type    TEXT NOT NULL DEFAULT '',
    -- subject_scope is whose request this is: a reseller, a region, a product,
    -- or — as the empty string — nobody. A read that names a subject names their
    -- scope with it, and the one read that deliberately does not says so in its
    -- own name.
    --
    -- It has no default, unlike the text columns around it. Their empty string
    -- is an absence — a request no operation drove, a subject whose type the
    -- caller did not name, an artifact never written — and a write that omits
    -- one means exactly that. This one's empty string is not an absence but a
    -- value, tenancy.Global(), so a default would hand the global scope to a
    -- write that forgot the column, filing a subject's request in the tenant
    -- that matches nobody — the mistake tenancy.Scope exists to make
    -- unspellable in Go. NOT NULL with nothing to fall back on makes that write
    -- fail instead. See the tenancy package.
    subject_scope   TEXT NOT NULL,
    -- The convention triple. created_at is when the request was submitted — the
    -- instant the statutory clock starts — and no longer wears a second name for
    -- the row's creation time. last_updated_at is NULL until the fulfiller first
    -- moves the request. archived_at is written by nothing in this package: a
    -- served request is reaped on its retention window rather than hidden, and
    -- the column is here because a table querygen can read a shape from has all
    -- three of these or none of them.
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

-- "What has been asked in this person's name." Leading with the subject rather
-- than the time is what makes List a range scan instead of a filter over every
-- request the system has ever served.
CREATE INDEX IF NOT EXISTS {{PREFIX}}dataprivacy_requests_subject_idx
    ON {{PREFIX}}dataprivacy_requests (subject_id, subject_scope, created_at, id);

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
