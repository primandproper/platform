CREATE TABLE IF NOT EXISTS {{PREFIX}}audit_log_entries (
    id            TEXT PRIMARY KEY,
    seq           BIGINT NOT NULL,
    scope         TEXT NOT NULL DEFAULT '',
    recorded_at   TIMESTAMPTZ NOT NULL,
    event_type    TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    actor_id      TEXT NOT NULL,
    actor_type    TEXT NOT NULL DEFAULT '',
    actor_ip      TEXT NOT NULL DEFAULT '',
    change_set    BYTEA,
    metadata      BYTEA,
    prev_hash     TEXT NOT NULL DEFAULT '',
    hash          TEXT NOT NULL
);

-- The chain's structural guarantee. Two writers racing on the same scope both
-- compute the same next position, and this index is what stops both from
-- committing: one transaction fails on the duplicate rather than both landing
-- and leaving a chain that forked. A fork is therefore not a thing Verify has
-- to detect, because it is not a thing the table can hold.
--
-- It also serves the chain-head read, which wants the highest seq in a scope.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}audit_log_entries_chain_idx
    ON {{PREFIX}}audit_log_entries (scope, seq);

-- Serves Verify's time-bounded walk and the retention sweep, both of which ask
-- for one scope's entries within a time range.
CREATE INDEX IF NOT EXISTS {{PREFIX}}audit_log_entries_scope_time_idx
    ON {{PREFIX}}audit_log_entries (scope, recorded_at);

-- "What did this principal do." Leading with the actor rather than the time is
-- what makes the answer a range scan instead of a filter over history.
CREATE INDEX IF NOT EXISTS {{PREFIX}}audit_log_entries_actor_idx
    ON {{PREFIX}}audit_log_entries (actor_id, recorded_at);

-- "What happened to this thing." Prefixed on resource_type so the same index
-- also answers the type-only question List supports.
CREATE INDEX IF NOT EXISTS {{PREFIX}}audit_log_entries_resource_idx
    ON {{PREFIX}}audit_log_entries (resource_type, resource_id, recorded_at);

-- One row per scope, holding that scope's chain head and how far retention has
-- pruned it.
--
-- The head is kept here rather than derived from the entries table on every
-- write because this row is what serializes concurrent writers: Record locks it
-- for the remainder of the caller's transaction, so the second writer waits and
-- then reads the head the first one committed. Locking the last entry row
-- instead cannot do that — the row a second writer would need to wait on is one
-- the first has not inserted yet.
--
-- It also survives pruning. Once retention deletes a scope's last surviving
-- entry there is nothing left to derive a head from, and a chain that restarted
-- at zero would collide with positions it had already used.
CREATE TABLE IF NOT EXISTS {{PREFIX}}audit_log_chains (
    scope               TEXT PRIMARY KEY,
    head_seq            BIGINT NOT NULL DEFAULT -1,
    head_hash           TEXT NOT NULL DEFAULT '',
    pruned_through_seq  BIGINT NOT NULL DEFAULT -1,
    pruned_through_hash TEXT NOT NULL DEFAULT '',
    updated_at          TIMESTAMPTZ NOT NULL
);
