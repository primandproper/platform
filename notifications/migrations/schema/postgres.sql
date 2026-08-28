CREATE TABLE IF NOT EXISTS notifications_inbox (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    principal       TEXT NOT NULL,
    topic           TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    link            TEXT NOT NULL DEFAULT '',
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS notifications_inbox_principal_idx
    ON notifications_inbox (scope, principal, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS notifications_inbox_unread_idx
    ON notifications_inbox (scope, principal, id)
    WHERE archived_at IS NULL AND read_at IS NULL;

CREATE TABLE IF NOT EXISTS notifications_devices (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL,
    principal    TEXT NOT NULL,
    platform     TEXT NOT NULL,
    token        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS notifications_devices_token_uniq
    ON notifications_devices (platform, token);

CREATE INDEX IF NOT EXISTS notifications_devices_principal_idx
    ON notifications_devices (scope, principal, id);

