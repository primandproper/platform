-- One table. A privacy request is a single row with a status, and the prior art
-- that split it across a request table and a disclosure table gained nothing
-- from the join except the possibility of the two disagreeing about whether an
-- artifact still existed.
CREATE TABLE IF NOT EXISTS {{PREFIX}}_requests (
    id              VARCHAR(64) NOT NULL PRIMARY KEY,
    request_type    VARCHAR(32) NOT NULL,
    status          VARCHAR(32) NOT NULL,
    subject_id      VARCHAR(255) NOT NULL,
    subject_type    VARCHAR(64) NOT NULL DEFAULT '',
    subject_scope   VARCHAR(255) NOT NULL DEFAULT '',
    requested_at    DATETIME(6) NOT NULL,
    due_at          DATETIME(6) NOT NULL,
    expires_at      DATETIME(6),
    completed_at    DATETIME(6),
    next_attempt    DATETIME(6) NOT NULL,
    claimed_until   DATETIME(6),
    attempts        INT NOT NULL DEFAULT 0,
    artifact_ref    TEXT NOT NULL,
    artifact_bytes  BIGINT NOT NULL DEFAULT 0,
    deleted_rows    BIGINT NOT NULL DEFAULT 0,
    anonymized_rows BIGINT NOT NULL DEFAULT 0,
    failures        BLOB,
    retained        BLOB,
    last_error      TEXT
);

-- MySQL has no partial indexes, so unlike the Postgres schema these cover the
-- whole table and the predicate columns lead. Every query these serve filters
-- on status first, so putting it in front keeps the index selective for the
-- same queries the partial clauses serve elsewhere.
CREATE INDEX {{PREFIX}}_requests_claim_idx
    ON {{PREFIX}}_requests (status, next_attempt, requested_at, id);

-- "What has been asked in this person's name." Leading with the subject rather
-- than the time is what makes List a range scan instead of a filter over every
-- request the system has ever served.
CREATE INDEX {{PREFIX}}_requests_subject_idx
    ON {{PREFIX}}_requests (subject_id, subject_scope, requested_at, id);

-- Serves both the artifact expiry sweep and the confirmation-window lapse
-- sweep; they differ only in the status they filter on, which leads the index.
CREATE INDEX {{PREFIX}}_requests_expiry_idx
    ON {{PREFIX}}_requests (status, expires_at);

-- Serves the overdue gauge.
CREATE INDEX {{PREFIX}}_requests_status_due_idx
    ON {{PREFIX}}_requests (status, due_at);

-- Serves the retention reap.
CREATE INDEX {{PREFIX}}_requests_reap_idx
    ON {{PREFIX}}_requests (completed_at, id);
