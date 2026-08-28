-- No convention triple, and that is deliberate. A reset token is issued, used
-- once, and gone; archived_at would keep rows nothing can ever read, and
-- last_updated_at would be a second copy of redeemed_at, since redemption is
-- the only mutation this row has. What reclaims the table is the sweeper, on
-- expires_at.
--
-- token_digest is the digest of the token, never the token. See postgres.sql
-- for why, and for what the three indexes serve.
--
-- scope is whose token it is, and it has no default. See postgres.sql, and the
-- tenancy package.
--
-- belongs_to_user carries no REFERENCES, unlike the equivalent column in the
-- identity schema, so that adopting the reset flow does not mean adopting
-- identity too.
CREATE TABLE IF NOT EXISTS {{PREFIX}}password_reset_tokens (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    belongs_to_user TEXT NOT NULL,
    token_digest    TEXT NOT NULL,
    expires_at      DATETIME NOT NULL,
    redeemed_at     DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}password_reset_tokens_digest_idx
    ON {{PREFIX}}password_reset_tokens (token_digest);

CREATE INDEX IF NOT EXISTS {{PREFIX}}password_reset_tokens_user_idx
    ON {{PREFIX}}password_reset_tokens (scope, belongs_to_user);

CREATE INDEX IF NOT EXISTS {{PREFIX}}password_reset_tokens_expires_at_idx
    ON {{PREFIX}}password_reset_tokens (expires_at);
