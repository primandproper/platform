-- No convention triple, and that is deliberate. A sweeper deletes expired rows
-- outright to keep this table sized by live sessions rather than by every
-- session ever opened, so archived_at would either do nothing or defeat the
-- sweep. last_seen_at, not last_updated_at, is the column a read moves, and it
-- is a liveness signal the store's policy compares against rather than a record
-- of the row's last mutation.
--
-- scope and principal are who holds the session: the tenant whose data it is,
-- and the identifier of the person or service inside that tenant. They are what
-- makes "which sessions do I have, and sign the others out" one index lookup
-- and one DELETE over this table rather than a second session table beside it —
-- which is two sources of truth about what a live session is, in the one place
-- a disagreement is a revocation that did not take.
--
-- Neither has a default, and scope's absence is the deliberate one. The empty
-- string is a scope, tenancy.Global(), so a column that supplies it for a write
-- which did not name one hands out the global scope to whoever forgot the
-- column. NOT NULL with nothing to fall back on makes that write fail instead.
-- See the tenancy package. principal follows it for symmetry: an unattributed
-- session names the empty principal on purpose, and it should have to say so.
--
-- The four device columns are the metadata a security page renders — enough for
-- a person to recognize a session of their own and to notice one that is not.
-- They are inherently a client's self-description and so are never trusted for
-- anything but display; the empty string is what a caller that knows none of it
-- writes.
CREATE TABLE IF NOT EXISTS {{PREFIX}}sessions (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL,
    principal    TEXT NOT NULL,
    data         BYTEA,
    device_name  TEXT NOT NULL,
    ip_address   TEXT NOT NULL,
    user_agent   TEXT NOT NULL,
    login_method TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    version      INTEGER NOT NULL
);

-- Serves the sweeper, which is the only query that reads a row it cannot name.
-- Every other statement in this package is keyed by the primary key or by the
-- pair below.
--
-- expires_at is written by the backend and read by nothing else: whether a
-- session is live is decided from created_at and last_seen_at against the
-- store's policy, so that both backends answer the question the same way. This
-- column exists so that dead rows can be found without scanning, and it is
-- deliberately not part of any read predicate — a clock skew between a writer
-- and a reader would otherwise hide live sessions.
CREATE INDEX IF NOT EXISTS {{PREFIX}}sessions_expires_at_idx
    ON {{PREFIX}}sessions (expires_at);

-- Serves the enumeration and the two bulk revocations, which are the same
-- lookup: this principal's rows within this scope. Leading with scope is what
-- keeps one tenant's list from walking another tenant's rows, and created_at
-- trailing the pair is the order the enumeration reads them back in — newest
-- first, which is how a security page lists them.
CREATE INDEX IF NOT EXISTS {{PREFIX}}sessions_principal_idx
    ON {{PREFIX}}sessions (scope, principal, created_at);
