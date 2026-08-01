-- One table. A saga instance is a cursor, a blob of state, and a status; the
-- prior art that gave each step its own row bought a step history nobody read
-- and a second place for the cursor to disagree with itself.
--
-- The step history that is worth having is the lifecycle event stream, which
-- goes through the outbox and lands wherever the application already keeps
-- events — not in a table this package would then have to sweep.
CREATE TABLE IF NOT EXISTS {{PREFIX}}saga_instances (
    id            TEXT PRIMARY KEY,
    definition    TEXT NOT NULL,
    status        TEXT NOT NULL,
    current_step  INTEGER NOT NULL DEFAULT 0,
    step_names    TEXT NOT NULL,
    state         BLOB,
    attempts      INTEGER NOT NULL DEFAULT 0,
    last_error    TEXT NOT NULL DEFAULT '',
    resume_status TEXT NOT NULL DEFAULT '',
    started_at    DATETIME NOT NULL,
    updated_at    DATETIME NOT NULL,
    next_attempt  DATETIME NOT NULL,
    claimed_until DATETIME
);

-- Serves the claim predicate. Partial on the two statuses a worker can advance,
-- so the index tracks the in-flight set rather than every saga the system has
-- ever run — the completed rows are the overwhelming majority and no claim ever
-- looks at them.
CREATE INDEX IF NOT EXISTS {{PREFIX}}saga_instances_claim_idx
    ON {{PREFIX}}saga_instances (next_attempt, started_at, id)
    WHERE status IN ('running', 'compensating');

-- Serves the operator read: "which sagas are stuck", and "what has this
-- definition been doing". Status leads because the question that gets asked at
-- three in the morning is scoped by status and not by definition.
CREATE INDEX IF NOT EXISTS {{PREFIX}}saga_instances_status_idx
    ON {{PREFIX}}saga_instances (status, definition, id);
