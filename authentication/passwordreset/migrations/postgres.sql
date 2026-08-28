-- No convention triple, and that is deliberate. A reset token is issued, used
-- once, and gone; archived_at would keep rows nothing can ever read, and
-- last_updated_at would be a second copy of redeemed_at, since redemption is
-- the only mutation this row has. What reclaims the table is the sweeper, on
-- expires_at.
--
-- token_digest is the digest of the token, never the token. A reset token is a
-- bearer credential for the account it names: a database copy — a backup, a
-- replica, a support engineer's query — is a password reset for every account
-- with an outstanding link if the column holds the raw value. It is not salted,
-- and does not need to be, because what it digests is thirty-two bytes of
-- randomness rather than something a person chose; there is no dictionary to
-- run against it.
--
-- scope is whose token it is: an account, an organization, a workspace, or — as
-- the empty string — nobody. Every read of this table filters on it.
--
-- It has no default, deliberately. The empty string is a scope, tenancy.Global(),
-- and a column that supplies it for a write which did not name one hands out the
-- global scope to whoever forgot the column — the mistake tenancy.Scope exists to
-- make unspellable in Go. NOT NULL with nothing to fall back on makes that write
-- fail instead. See the tenancy package.
--
-- belongs_to_user carries no REFERENCES, unlike the equivalent column in the
-- identity schema. This package is usable by an application that keeps its users
-- somewhere this table cannot name — another schema, another service, or a
-- users table of its own predating this module — and a foreign key would make
-- adopting the reset flow mean adopting identity too.
CREATE TABLE IF NOT EXISTS {{PREFIX}}password_reset_tokens (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    belongs_to_user TEXT NOT NULL,
    token_digest    TEXT NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    redeemed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves the lookup every verification and every redemption makes, and is UNIQUE
-- rather than merely indexed. Two live rows with one digest would be one link
-- that unlocks two accounts, so the constraint is the schema saying what the
-- generator's thirty-two bytes already make astronomically unlikely — and a
-- generator swapped for a weaker one fails its insert instead of issuing a
-- collision.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}password_reset_tokens_digest_idx
    ON {{PREFIX}}password_reset_tokens (token_digest);

-- Serves the revocation a completed reset runs, which is the one statement here
-- that names a user rather than a token. Leading with scope keeps one tenant's
-- revocation from walking every other tenant's rows.
CREATE INDEX IF NOT EXISTS {{PREFIX}}password_reset_tokens_user_idx
    ON {{PREFIX}}password_reset_tokens (scope, belongs_to_user);

-- Serves the sweeper, which is the only statement in this package that reads
-- rows it cannot name.
--
-- expires_at is also what decides liveness, and it decides it in Go rather than
-- in this predicate: a row the sweeper has not reached yet is already refused,
-- so the sweep is what keeps the table from growing by a row per forgotten
-- password forever rather than what makes a link stop working.
CREATE INDEX IF NOT EXISTS {{PREFIX}}password_reset_tokens_expires_at_idx
    ON {{PREFIX}}password_reset_tokens (expires_at);
