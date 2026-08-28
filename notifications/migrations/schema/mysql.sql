CREATE TABLE IF NOT EXISTS notifications_inbox (
    id              VARCHAR(64) NOT NULL PRIMARY KEY,
    scope           VARCHAR(255) NOT NULL,
    principal       VARCHAR(64) NOT NULL,
    topic           VARCHAR(255) NOT NULL,
    title           VARCHAR(255) NOT NULL,
    body            VARCHAR(2048) NOT NULL DEFAULT '',
    link            VARCHAR(2048) NOT NULL DEFAULT '',
    read_at         DATETIME(6),
    created_at      DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_updated_at DATETIME(6),
    archived_at     DATETIME(6)
);

CREATE INDEX notifications_inbox_principal_idx
    ON notifications_inbox (scope, principal, archived_at, id);

CREATE INDEX notifications_inbox_unread_idx
    ON notifications_inbox (scope, principal, archived_at, read_at, id);

CREATE TABLE IF NOT EXISTS notifications_devices (
    id           VARCHAR(64) NOT NULL PRIMARY KEY,
    scope        VARCHAR(255) NOT NULL,
    principal    VARCHAR(64) NOT NULL,
    platform     VARCHAR(32) NOT NULL,
    token        VARCHAR(512) NOT NULL,
    last_seen_at DATETIME(6) NOT NULL,
    created_at   DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY notifications_devices_token_uniq (platform, token)
);

CREATE INDEX notifications_devices_principal_idx
    ON notifications_devices (scope, principal, id);

