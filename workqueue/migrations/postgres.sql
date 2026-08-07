CREATE TABLE IF NOT EXISTS {{PREFIX}}work_queue_items (
    queue_name   TEXT        NOT NULL,
    item_key     TEXT        NOT NULL,
    priority     INTEGER     NOT NULL DEFAULT 0,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    enqueued_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Never-leased and lease-lapsed are the same state to every reader, so
    -- lease_until is NOT NULL and starts at the epoch rather than being
    -- nullable. The claim predicate is then one comparison instead of a
    -- comparison plus a NULL branch that every future writer has to remember.
    lease_until  TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    completed_at TIMESTAMPTZ,
    last_error   TEXT,

    -- One table serves every logical queue in the database, so the queue name
    -- leads the key. It is also the lock order every writer of this table
    -- acquires rows in.
    PRIMARY KEY (queue_name, item_key)
);

-- Serves the claim: the predicate filters on queue and completion, and the
-- ORDER BY is (priority DESC, available_at, item_key). The partial clause is
-- what keeps this index sized by backlog rather than by total history —
-- completed rows outnumber pending ones by orders of magnitude between reaps,
-- and without it a claim slows down as the table fills.
CREATE INDEX IF NOT EXISTS {{PREFIX}}work_queue_items_claim_idx
    ON {{PREFIX}}work_queue_items (queue_name, priority DESC, available_at, item_key)
    WHERE completed_at IS NULL;

-- Serves the reaper, and nothing else looks at completed rows.
CREATE INDEX IF NOT EXISTS {{PREFIX}}work_queue_items_reap_idx
    ON {{PREFIX}}work_queue_items (queue_name, completed_at)
    WHERE completed_at IS NOT NULL;
