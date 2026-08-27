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
-- No convention triple here, and that is deliberate. A retention sweep deletes
-- these rows outright once they age past the window, so archived_at would either
-- do nothing or keep the table growing forever; recorded_at is the retention key
-- rather than a creation stamp, and the row is written once and never updated,
-- so there is no last mutation for last_updated_at to record. The totals table
-- below is the one meant to be kept, and it carries the triple.
CREATE TABLE IF NOT EXISTS {{PREFIX}}metering_events (
    -- Dedupe is an INSERT that either takes or does not, decided by the database
    -- in one round trip, and durable for as long as the row is retained. A
    -- cache-backed dedupe would be correct only for as long as the cache held,
    -- and a billing period is longer than any cache TTL anybody sets.
    --
    -- The primary key is (meter, idempotency_key), not the key alone. Callers
    -- are told to use a request ID as the idempotency key, and one request
    -- routinely feeds more than one meter — an API call that bills both a
    -- request count and a byte count. Keyed on the key alone the second meter's
    -- insert is silently deduped against the first, and the customer is
    -- under-billed for it forever.
    --
    -- VARCHAR(255) rather than TEXT because MySQL cannot index a TEXT column
    -- without a prefix length, and a prefix-indexed primary key would dedupe on
    -- the first N bytes — quietly discarding usage whose keys share a prefix.
    idempotency_key VARCHAR(255) NOT NULL,
    subject         VARCHAR(255) NOT NULL,
    meter           VARCHAR(64) NOT NULL,
    quantity        BIGINT NOT NULL,
    occurred_at     DATETIME(6) NOT NULL,
    recorded_at     DATETIME(6) NOT NULL,
    period_start    DATETIME(6) NOT NULL,
    dimensions      BLOB,

    PRIMARY KEY (meter, idempotency_key)
);

-- Serves the retention reap, which asks one question about time and nothing
-- else, and the per-period event listing behind a usage breakdown.
CREATE INDEX {{PREFIX}}metering_events_period_idx
    ON {{PREFIX}}metering_events (subject, meter, period_start, occurred_at);

CREATE INDEX {{PREFIX}}metering_events_reap_idx
    ON {{PREFIX}}metering_events (recorded_at);

CREATE TABLE IF NOT EXISTS {{PREFIX}}metering_totals (
    subject          VARCHAR(255) NOT NULL,
    meter            VARCHAR(64) NOT NULL,
    period_start     DATETIME(6) NOT NULL,
    period_end       DATETIME(6) NOT NULL,
    aggregation      VARCHAR(32) NOT NULL,
    quantity         BIGINT NOT NULL DEFAULT 0,
    -- The event time of the newest record folded in. AggregationLast orders by
    -- it, so a record that arrives late does not displace a newer one — which is
    -- the whole difference between "last" and "most recently ingested".
    last_occurred_at DATETIME(6) NOT NULL,
    -- How much of quantity the provider has already been told about, and how
    -- many times it has been told. The sequence is the varying component of the
    -- provider-side idempotency key: a retried post reuses it and is a no-op, and
    -- a genuinely new post gets a fresh one.
    flushed_quantity BIGINT NOT NULL DEFAULT 0,
    flush_sequence   INT NOT NULL DEFAULT 0,
    flush_attempts   INT NOT NULL DEFAULT 0,
    next_flush       DATETIME(6) NOT NULL,
    claimed_until    DATETIME(6),
    last_error       TEXT,
    created_at       DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at  DATETIME(6),
    archived_at      DATETIME(6),
    PRIMARY KEY (subject, meter, period_start)
);

-- MySQL has no partial indexes, so unlike the Postgres schema this covers the
-- whole table. next_flush leads because it is the only column of the claim
-- predicate that is selective — the quantity > flushed_quantity comparison is
-- between two columns and no index can serve it anywhere.
CREATE INDEX {{PREFIX}}metering_totals_flush_idx
    ON {{PREFIX}}metering_totals (next_flush, subject, meter);

-- Serves "what has this subject used lately", across meters.
CREATE INDEX {{PREFIX}}metering_totals_subject_idx
    ON {{PREFIX}}metering_totals (subject, period_start, meter);
