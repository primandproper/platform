CREATE TABLE IF NOT EXISTS waitlists (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    closes_at       DATETIME NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

CREATE INDEX IF NOT EXISTS waitlists_scope_idx
    ON waitlists (scope, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS waitlists_open_idx
    ON waitlists (scope, closes_at, id)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS waitlist_signups (
    id                TEXT PRIMARY KEY,
    scope             TEXT NOT NULL,
    waitlist_id       TEXT NOT NULL REFERENCES waitlists (id) ON DELETE CASCADE,
    contact           TEXT NOT NULL DEFAULT '',
    contact_digest    TEXT NOT NULL,
    subject_type      TEXT NOT NULL DEFAULT '',
    subject_id        TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL,
    status_changed_at DATETIME,
    created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at   DATETIME,
    archived_at       DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS waitlist_signups_contact_uniq
    ON waitlist_signups (scope, waitlist_id, contact_digest);

CREATE INDEX IF NOT EXISTS waitlist_signups_waitlist_idx
    ON waitlist_signups (scope, waitlist_id, id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS waitlist_signups_subject_idx
    ON waitlist_signups (scope, subject_type, subject_id, id)
    WHERE archived_at IS NULL;

