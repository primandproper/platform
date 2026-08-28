-- MySQL has no CREATE INDEX IF NOT EXISTS, so the three indexes are declared
-- inline. See postgres.sql for what each one serves.
--
-- The TEXT columns are VARCHAR here because every one of them is indexed, and
-- MySQL cannot index a TEXT column without a prefix length. id is VARCHAR(64)
-- to match the rest of this module's identifiers; token_digest is VARCHAR(255)
-- rather than the 64 characters a hex SHA-256 occupies, so that a deployment
-- configuring a wider hasher does not need a migration to store its output.
--
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
-- scope is whose token it is, and it has no default. See postgres.sql, and the
-- tenancy package.
--
-- belongs_to_user carries no REFERENCES, unlike the equivalent column in the
-- identity schema, so that adopting the reset flow does not mean adopting
-- identity too.
CREATE TABLE IF NOT EXISTS {{PREFIX}}password_reset_tokens (
    id              VARCHAR(64)  NOT NULL PRIMARY KEY,
    scope           VARCHAR(255) NOT NULL,
    belongs_to_user VARCHAR(255) NOT NULL,
    token_digest    VARCHAR(255) NOT NULL,
    expires_at      DATETIME(6)  NOT NULL,
    redeemed_at     DATETIME(6),
    created_at      DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),

    UNIQUE KEY {{PREFIX}}password_reset_tokens_digest_idx (token_digest),
    KEY {{PREFIX}}password_reset_tokens_user_idx (scope, belongs_to_user),
    KEY {{PREFIX}}password_reset_tokens_expires_at_idx (expires_at)
);
