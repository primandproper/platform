-- MySQL has neither partial indexes nor CREATE INDEX IF NOT EXISTS, so the
-- indexes are declared inline. The string columns are VARCHAR rather than TEXT
-- for two reasons that both bite here: TEXT cannot carry a DEFAULT, and TEXT
-- cannot be indexed without a prefix length — and a prefix-length index on
-- scope would not enforce the chain's uniqueness, which is the one index in
-- this schema that is load-bearing rather than an optimization.
--
-- See postgres.sql for what each table and index is for.
CREATE TABLE IF NOT EXISTS {{PREFIX}}audit_log_entries (
    id            VARCHAR(64)  NOT NULL PRIMARY KEY,
    seq           BIGINT       NOT NULL,
    scope         VARCHAR(255) NOT NULL DEFAULT '',
    recorded_at   DATETIME(6)  NOT NULL,
    event_type    VARCHAR(255) NOT NULL,
    resource_type VARCHAR(255) NOT NULL,
    resource_id   VARCHAR(255) NOT NULL DEFAULT '',
    actor_id      VARCHAR(255) NOT NULL,
    actor_type    VARCHAR(64)  NOT NULL DEFAULT '',
    actor_ip      VARCHAR(64)  NOT NULL DEFAULT '',
    change_set    LONGBLOB     NULL,
    metadata      LONGBLOB     NULL,
    prev_hash     VARCHAR(64)  NOT NULL DEFAULT '',
    hash          VARCHAR(64)  NOT NULL,

    UNIQUE KEY {{PREFIX}}audit_log_entries_chain_idx (scope, seq),
    KEY {{PREFIX}}audit_log_entries_scope_time_idx (scope, recorded_at),
    KEY {{PREFIX}}audit_log_entries_actor_idx (actor_id, recorded_at),
    KEY {{PREFIX}}audit_log_entries_resource_idx (resource_type, resource_id, recorded_at)
);

CREATE TABLE IF NOT EXISTS {{PREFIX}}audit_log_chains (
    scope               VARCHAR(255) NOT NULL PRIMARY KEY,
    head_seq            BIGINT       NOT NULL DEFAULT -1,
    head_hash           VARCHAR(64)  NOT NULL DEFAULT '',
    pruned_through_seq  BIGINT       NOT NULL DEFAULT -1,
    pruned_through_hash VARCHAR(64)  NOT NULL DEFAULT '',
    updated_at          DATETIME(6)  NOT NULL
);
