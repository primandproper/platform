-- MySQL has neither partial indexes nor CREATE INDEX IF NOT EXISTS, so the
-- indexes are declared inline and lead with the columns the claim predicate
-- filters on. They cover more rows than the Postgres equivalents; the reaper
-- keeps that bounded.
CREATE TABLE IF NOT EXISTS {{TABLE}} (
    id            VARCHAR(64)  NOT NULL PRIMARY KEY,
    topic         VARCHAR(255) NOT NULL,
    partition_key VARCHAR(255) NOT NULL DEFAULT '',
    payload       LONGBLOB     NOT NULL,
    created_at    DATETIME(6)  NOT NULL,
    next_attempt  DATETIME(6)  NOT NULL,
    claimed_until DATETIME(6)  NULL,
    published_at  DATETIME(6)  NULL,
    attempts      INT          NOT NULL DEFAULT 0,
    last_error    TEXT         NULL,
    quarantined   BOOLEAN      NOT NULL DEFAULT FALSE,

    KEY {{TABLE}}_claim_idx (published_at, quarantined, next_attempt, created_at),
    KEY {{TABLE}}_ordering_idx (partition_key, published_at, quarantined, created_at, id),
    KEY {{TABLE}}_reap_idx (published_at)
);
