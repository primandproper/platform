-- One table. An operation is a state, a progress reading, and an outcome, and
-- the prior art that gave progress its own rows bought an append-only history
-- nobody read and a second place for "how far along is this" to disagree with
-- itself.
--
-- The history that is worth having is the sequence of snapshots a watcher
-- already receives, which lands wherever the application keeps its events —
-- not in a table this package would then have to sweep at one row per progress
-- flush per operation.
CREATE TABLE IF NOT EXISTS {{PREFIX}}operations (
    id               TEXT        PRIMARY KEY,
    kind             TEXT        NOT NULL,
    state            TEXT        NOT NULL,
    -- Opaque to this package and compared only for equality. NOT NULL with an
    -- empty default rather than nullable, so "unowned" is one value and every
    -- scoped read is one comparison.
    owner            TEXT        NOT NULL DEFAULT '',
    -- Nullable rather than defaulting to an empty array: "no request" and "an
    -- empty request" are different statements about the operation, and a Runner
    -- that branches on one should not be handed the other.
    request          BYTEA,

    -- Progress, in two tiers. units_total is the only nullable one of them,
    -- because "there is no denominator" is a fact about the work that a zero
    -- could not distinguish from "there are no units yet".
    units_total      INTEGER,
    units_done       INTEGER     NOT NULL DEFAULT 0,
    progress_unit    TEXT        NOT NULL DEFAULT '',
    progress_count   BIGINT      NOT NULL DEFAULT 0,
    -- Written once at insert from the kind's registration, so a row remains a
    -- complete answer to a reader that has no registry — the watcher, an
    -- operator with psql.
    count_label      TEXT        NOT NULL DEFAULT '',
    progress_message TEXT        NOT NULL DEFAULT '',

    -- The outcome. Both halves are always present as columns and only one of
    -- them is ever read, decided by state; see scanOperation.
    result_uri       TEXT        NOT NULL DEFAULT '',
    result_detail    BYTEA,
    error_code       TEXT        NOT NULL DEFAULT '',
    error_message    TEXT        NOT NULL DEFAULT '',
    error_retryable  BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Incremented by every write. It is what lets a watcher decide whether the
    -- row it just re-read is new without comparing every column, and it is the
    -- one field a future column addition cannot make stale.
    revision         BIGINT      NOT NULL DEFAULT 1,
    attempts         INTEGER     NOT NULL DEFAULT 0,
    cancel_requested BOOLEAN     NOT NULL DEFAULT FALSE,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Null until a worker picks the operation up, and null until it finishes.
    -- The gap between created_at and started_at is queue latency, which is the
    -- number that explains a slow export that ran quickly.
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    -- Never-claimed and lease-lapsed are the same state to every reader, so
    -- claimed_until is NOT NULL and starts at the epoch rather than being
    -- nullable. The claimability predicate is then one comparison instead of a
    -- comparison plus a NULL branch that every future writer has to remember.
    claimed_until    TIMESTAMPTZ NOT NULL DEFAULT 'epoch'
);

-- Serves the recovery sweep, which is the only read that scans by time rather
-- than by key. The partial clause is what keeps it sized by in-flight work
-- rather than by everything the system has ever run — terminal rows outnumber
-- active ones by orders of magnitude between reaps, and no sweep ever looks at
-- them.
CREATE INDEX IF NOT EXISTS {{PREFIX}}operations_active_idx
    ON {{PREFIX}}operations (updated_at, claimed_until)
    WHERE state IN ('pending', 'running');

-- Serves the API read: "what has this account got running", and its narrowing
-- by kind and state. Owner leads because every request-scoped listing is scoped
-- by it, and id trails so the cursor pagination reads off the index.
CREATE INDEX IF NOT EXISTS {{PREFIX}}operations_owner_idx
    ON {{PREFIX}}operations (owner, kind, state, id);

-- Serves the reaper, and nothing else looks at finished rows by time.
CREATE INDEX IF NOT EXISTS {{PREFIX}}operations_reap_idx
    ON {{PREFIX}}operations (finished_at)
    WHERE state IN ('succeeded', 'failed', 'cancelled');
