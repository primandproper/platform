-- Two tables, and the split is the difference between a fact about a person and
-- a fact about a handset. The Postgres schema carries the long form of why.
CREATE TABLE IF NOT EXISTS {{PREFIX}}notifications_inbox (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    principal       TEXT NOT NULL,
    topic           TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    link            TEXT NOT NULL DEFAULT '',
    read_at         DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}notifications_inbox_principal_idx
    ON {{PREFIX}}notifications_inbox (scope, principal, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS {{PREFIX}}notifications_inbox_unread_idx
    ON {{PREFIX}}notifications_inbox (scope, principal, id)
    WHERE archived_at IS NULL AND read_at IS NULL;

-- The device registry. No convention triple beyond created_at: a token is
-- revoked by its owner or invalidated by the provider, and either way the row
-- goes — see the Postgres schema.
CREATE TABLE IF NOT EXISTS {{PREFIX}}notifications_devices (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL,
    principal    TEXT NOT NULL,
    platform     TEXT NOT NULL,
    token        TEXT NOT NULL,
    last_seen_at DATETIME NOT NULL,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- One row per live token, across every scope and principal, which is what makes
-- re-registration a move rather than a fan-out — see the Postgres schema. It is
-- also the conflict target the registration upsert names.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}notifications_devices_token_uniq
    ON {{PREFIX}}notifications_devices (platform, token);

CREATE INDEX IF NOT EXISTS {{PREFIX}}notifications_devices_principal_idx
    ON {{PREFIX}}notifications_devices (scope, principal, id);
