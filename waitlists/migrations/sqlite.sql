-- scope is whose waitlists these are: a reseller, a region, a product, or — as
-- the empty string — nobody. Every read of every table here filters on it.
--
-- It has no default. The empty string is a scope, tenancy.Global(), and a
-- column that supplied it for a write which did not name one would hand out the
-- global scope to whoever forgot the column — the mistake tenancy.Scope exists
-- to make unspellable in Go. NOT NULL with nothing to fall back on makes that
-- write fail instead. See the tenancy package.
CREATE TABLE IF NOT EXISTS {{PREFIX}}waitlists (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    closes_at       DATETIME NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

-- closes_at is NOT NULL in every dialect — see the Postgres schema for why a
-- list that never closes is a list that is archived instead.

-- Serves the catalog page: one scope, live rows, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlists_scope_idx
    ON {{PREFIX}}waitlists (scope, id)
    WHERE archived_at IS NULL;

-- Serves the other page: the lists still taking signups.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlists_open_idx
    ON {{PREFIX}}waitlists (scope, closes_at, id)
    WHERE archived_at IS NULL;

-- One person's place on one list. contact is the address the list writes to and
-- contact_digest is what the row is found by and what survives a withdrawal —
-- see the Postgres schema.
CREATE TABLE IF NOT EXISTS {{PREFIX}}waitlist_signups (
    id                TEXT PRIMARY KEY,
    scope             TEXT NOT NULL,
    waitlist_id       TEXT NOT NULL REFERENCES {{PREFIX}}waitlists (id) ON DELETE CASCADE,
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

-- One signup per contact per list, live and archived alike — which is what
-- makes a withdrawal a suppression rather than a row somebody can write past.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}waitlist_signups_contact_uniq
    ON {{PREFIX}}waitlist_signups (scope, waitlist_id, contact_digest);

-- Serves the queue: one list's live signups, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlist_signups_waitlist_idx
    ON {{PREFIX}}waitlist_signups (scope, waitlist_id, id)
    WHERE archived_at IS NULL;

-- Serves "which lists is this person on".
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlist_signups_subject_idx
    ON {{PREFIX}}waitlist_signups (scope, subject_type, subject_id, id)
    WHERE archived_at IS NULL;
