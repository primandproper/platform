-- scope is whose waitlists these are: a reseller, a region, a product, or — as
-- the empty string — nobody. Every read of every table here filters on it.
--
-- It has no default. The empty string is a scope, tenancy.Global(), and a
-- column that supplied it for a write which did not name one would hand out the
-- global scope to whoever forgot the column — the mistake tenancy.Scope exists
-- to make unspellable in Go. NOT NULL with nothing to fall back on makes that
-- write fail instead. See the tenancy package.
--
-- A list and the signups against it share a scope. The signup carries its own
-- copy rather than reaching the list's through the reference, because every
-- read of a signup filters on the scope and a predicate that had to join to
-- find it would be a predicate a read could omit.
CREATE TABLE IF NOT EXISTS {{PREFIX}}waitlists (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    closes_at       TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

-- closes_at is NOT NULL, which is the one column in this schema a reader is
-- entitled to argue about, so the reason is here rather than implied.
--
-- A nullable closing time reads as "this list never closes", and the read that
-- would have to honor it is `closes_at IS NULL OR closes_at > now` — a
-- disjunction over the same column, on the paged read this package exists to
-- serve, on three dialects. What the column buys instead is one comparison
-- against a bound instant, which is what makes "the lists taking signups right
-- now" a keyset page rather than a filter applied after the fact.
--
-- The state the nullable column was for is still expressible, and it is the
-- state the row already had: a list whose end is not yet decided names a far
-- horizon and is brought in by an update when the date is known. A list that
-- should stop taking signups this instant is archived, which is the retirement
-- this schema already has. See the waitlists package documentation.

-- Serves the catalog page: one scope, live rows, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlists_scope_idx
    ON {{PREFIX}}waitlists (scope, id)
    WHERE archived_at IS NULL;

-- Serves the other page: the lists still taking signups, which is the catalog
-- page with one more comparison and the same walk.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlists_open_idx
    ON {{PREFIX}}waitlists (scope, closes_at, id)
    WHERE archived_at IS NULL;

-- One person's place on one list.
--
-- contact and contact_digest are the pair this table is shaped around, and they
-- are two columns rather than one because they answer to two different
-- obligations. contact is the address the list exists to write to, so it is
-- stored as itself: a digest cannot be emailed. contact_digest is what the row
-- is found by — the uniqueness below, the "am I already on this list" read, the
-- unsubscribe — and it is what survives a withdrawal.
--
-- That last part is the whole design. Honoring "take me off this list" means
-- remembering the person after their address has been erased, and a digest is
-- how a table remembers somebody it no longer holds. So a withdrawal blanks
-- contact, notes and the subject reference, and leaves the digest; a later
-- signup by the same address finds the withdrawn row and is refused rather than
-- quietly re-subscribing whoever asked to be left alone. See the waitlists
-- package documentation, and passwordreset for the digest decision this one is
-- modeled on.
--
-- contact is NOT NULL DEFAULT '' rather than nullable, in the module's habit:
-- the empty string is an erased contact, and there is no third state for a
-- nullable column to carry. The digest has no default, because a row without
-- one is a signup nothing can ever suppress.
--
-- subject_type and subject_id are two columns rather than one composite string
-- for the reason the tenancy doctrine gives for the scope: a key spelling
-- "user:abc123" carries two facts in a column that can only be indexed as one.
-- Both default to the empty string, which is a signup that names nobody — the
-- ordinary case for a pre-launch list, where an address is all there is.
--
-- status_changed_at is nullable and is not last_updated_at. last_updated_at
-- answers "when did this row last change", and an administrator fixing a typo
-- in notes changes the row without changing where anybody stands. The reminder
-- that goes out three days after an invitation is scheduled off this column,
-- and it must not be reset by an edit that moved nobody.
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
    status_changed_at TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at   TIMESTAMPTZ,
    archived_at       TIMESTAMPTZ
);

-- One signup per contact per list, live and archived alike — no partial clause.
--
-- The uniqueness covers archived and withdrawn rows because those are the rows
-- it is most for. A partial index would free the key the moment somebody
-- withdrew, and the next signup from that address would insert a second row —
-- which is the suppression failing silently, on the one obligation this table
-- carries that is somebody else's to enforce on us.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}waitlist_signups_contact_uniq
    ON {{PREFIX}}waitlist_signups (scope, waitlist_id, contact_digest);

-- Serves the queue: one list's live signups, walked by id, which is also the
-- order they joined in.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlist_signups_waitlist_idx
    ON {{PREFIX}}waitlist_signups (scope, waitlist_id, id)
    WHERE archived_at IS NULL;

-- Serves "which lists is this person on", which is the read a profile page and
-- a data privacy export both make.
CREATE INDEX IF NOT EXISTS {{PREFIX}}waitlist_signups_subject_idx
    ON {{PREFIX}}waitlist_signups (scope, subject_type, subject_id, id)
    WHERE archived_at IS NULL;
