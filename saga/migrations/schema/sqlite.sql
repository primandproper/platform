CREATE TABLE IF NOT EXISTS saga_instances (
    id              TEXT PRIMARY KEY,
    definition      TEXT NOT NULL,
    status          TEXT NOT NULL,
    current_step    INTEGER NOT NULL DEFAULT 0,
    step_names      TEXT NOT NULL,
    state           BLOB,
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT NOT NULL DEFAULT '',
    resume_status   TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME,
    next_attempt    DATETIME NOT NULL,
    claimed_until   DATETIME
);

CREATE INDEX IF NOT EXISTS saga_instances_claim_idx
    ON saga_instances (next_attempt, created_at, id)
    WHERE status IN ('running', 'compensating');

CREATE INDEX IF NOT EXISTS saga_instances_status_idx
    ON saga_instances (status, definition, id);

