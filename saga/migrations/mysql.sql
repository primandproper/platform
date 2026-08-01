-- One table. A saga instance is a cursor, a blob of state, and a status; the
-- prior art that gave each step its own row bought a step history nobody read
-- and a second place for the cursor to disagree with itself.
--
-- The step history that is worth having is the lifecycle event stream, which
-- goes through the outbox and lands wherever the application already keeps
-- events — not in a table this package would then have to sweep.
CREATE TABLE IF NOT EXISTS {{PREFIX}}saga_instances (
    id            VARCHAR(64) NOT NULL PRIMARY KEY,
    definition    VARCHAR(255) NOT NULL,
    status        VARCHAR(32) NOT NULL,
    current_step  INT NOT NULL DEFAULT 0,
    step_names    TEXT NOT NULL,
    state         BLOB,
    attempts      INT NOT NULL DEFAULT 0,
    last_error    TEXT NOT NULL,
    resume_status VARCHAR(32) NOT NULL DEFAULT '',
    started_at    DATETIME(6) NOT NULL,
    updated_at    DATETIME(6) NOT NULL,
    next_attempt  DATETIME(6) NOT NULL,
    claimed_until DATETIME(6)
);

-- MySQL has no partial indexes, so unlike the Postgres schema these cover the
-- whole table and the status column leads. Both queries these serve filter on
-- status first, so putting it in front keeps the index selective for the same
-- reads the partial clauses serve elsewhere.
CREATE INDEX {{PREFIX}}saga_instances_claim_idx
    ON {{PREFIX}}saga_instances (status, next_attempt, started_at, id);

-- Serves the operator read: "which sagas are stuck", and "what has this
-- definition been doing".
CREATE INDEX {{PREFIX}}saga_instances_status_idx
    ON {{PREFIX}}saga_instances (status, definition, id);
