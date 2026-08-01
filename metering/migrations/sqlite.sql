-- Two tables, and the split is load-bearing rather than normalization for its
-- own sake.
--
-- The events table is the ingest ledger: one row per idempotency key, which is
-- what makes counting exactly-once. It is written once and never updated, and it
-- is the evidence behind an invoice when somebody disputes one.
--
-- The totals table is the aggregate the read path and the flusher use. It is
-- small — one row per subject, meter, and period — and it is the only thing
-- Consume locks. Deriving it from the events table on every read would be a
-- group-by over a table that grows with traffic, on the path this package exists
-- to keep cheap.
CREATE TABLE IF NOT EXISTS {{PREFIX}}_events (
    -- The idempotency key IS the primary key. Dedupe is therefore an INSERT that
    -- either takes or does not, decided by the database in one round trip, and
    -- durable for as long as the row is retained. A cache-backed dedupe would be
    -- correct for as long as the cache held, and a billing period is longer than
    -- any cache TTL anybody sets.
    idempotency_key TEXT PRIMARY KEY,
    subject         TEXT NOT NULL,
    meter           TEXT NOT NULL,
    quantity        INTEGER NOT NULL,
    occurred_at     DATETIME NOT NULL,
    recorded_at     DATETIME NOT NULL,
    period_start    DATETIME NOT NULL,
    dimensions      BLOB
);

-- Serves the retention reap, which asks one question about time and nothing
-- else, and the per-period event listing behind a usage breakdown.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_events_period_idx
    ON {{PREFIX}}_events (subject, meter, period_start, occurred_at);

CREATE INDEX IF NOT EXISTS {{PREFIX}}_events_reap_idx
    ON {{PREFIX}}_events (recorded_at);

CREATE TABLE IF NOT EXISTS {{PREFIX}}_totals (
    subject          TEXT NOT NULL,
    meter            TEXT NOT NULL,
    period_start     DATETIME NOT NULL,
    period_end       DATETIME NOT NULL,
    aggregation      TEXT NOT NULL,
    quantity         INTEGER NOT NULL DEFAULT 0,
    -- The event time of the newest record folded in. AggregationLast orders by
    -- it, so a record that arrives late does not displace a newer one — which is
    -- the whole difference between "last" and "most recently ingested".
    last_occurred_at DATETIME NOT NULL,
    -- How much of quantity the provider has already been told about, and how
    -- many times it has been told. The sequence is the varying component of the
    -- provider-side idempotency key: a retried post reuses it and is a no-op, and
    -- a genuinely new post gets a fresh one.
    flushed_quantity INTEGER NOT NULL DEFAULT 0,
    flush_sequence   INTEGER NOT NULL DEFAULT 0,
    flush_attempts   INTEGER NOT NULL DEFAULT 0,
    next_flush       DATETIME NOT NULL,
    claimed_until    DATETIME,
    last_error       TEXT NOT NULL DEFAULT '',
    updated_at       DATETIME NOT NULL,
    PRIMARY KEY (subject, meter, period_start)
);

-- Serves the flush claim: totals that owe the provider something, whose retry
-- time has come, and which nobody currently holds. Partial on the one predicate
-- that matters, so the index tracks the flush backlog rather than the history of
-- every period ever billed — this table is meant to be kept for years.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_totals_flush_idx
    ON {{PREFIX}}_totals (next_flush, subject, meter)
    WHERE quantity > flushed_quantity;

-- Serves "what has this subject used lately", across meters.
CREATE INDEX IF NOT EXISTS {{PREFIX}}_totals_subject_idx
    ON {{PREFIX}}_totals (subject, period_start, meter);
