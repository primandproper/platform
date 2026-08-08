CREATE TABLE IF NOT EXISTS {{PREFIX}}scheduled_timers (
    timer_set    TEXT        NOT NULL,
    timer_key    TEXT        NOT NULL,
    -- The schedule, and the whole reason this table is not a work queue: an
    -- absolute instant, agreed between every process, rather than an offset
    -- from whichever one happened to write the row.
    run_at       TIMESTAMPTZ NOT NULL,
    -- Nullable rather than defaulting to an empty array: "no payload" and "an
    -- empty payload" are different statements about the timer, and a handler
    -- that branches on one should not be handed the other.
    payload      BYTEA,
    attempts     INTEGER     NOT NULL DEFAULT 0,
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Never-leased and lease-lapsed are the same state to every reader, so
    -- lease_until is NOT NULL and starts at the epoch rather than being
    -- nullable. The due predicate is then one comparison instead of a
    -- comparison plus a NULL branch that every future writer has to remember.
    lease_until  TIMESTAMPTZ NOT NULL DEFAULT 'epoch',
    fired_at     TIMESTAMPTZ,
    last_error   TEXT,

    -- One table serves every logical timer set in the database, so the set name
    -- leads the key. It is also the lock order every writer of this table
    -- acquires rows in.
    PRIMARY KEY (timer_set, timer_key)
);

-- Serves the claim, whose predicate filters on set and firing and whose ORDER BY
-- is (run_at, timer_key), and the head of the next-due read. The partial clause
-- is what keeps this index sized by outstanding timers rather than by total
-- history — fired rows outnumber pending ones by orders of magnitude between
-- reaps, and without it firing slows down as the table fills.
CREATE INDEX IF NOT EXISTS {{PREFIX}}scheduled_timers_due_idx
    ON {{PREFIX}}scheduled_timers (timer_set, run_at, timer_key)
    WHERE fired_at IS NULL;

-- Serves the reaper, and nothing else looks at fired rows.
CREATE INDEX IF NOT EXISTS {{PREFIX}}scheduled_timers_reap_idx
    ON {{PREFIX}}scheduled_timers (timer_set, fired_at)
    WHERE fired_at IS NOT NULL;
