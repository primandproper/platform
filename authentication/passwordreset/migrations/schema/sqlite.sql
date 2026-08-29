CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    belongs_to_user TEXT NOT NULL,
    token_digest    TEXT NOT NULL,
    expires_at      DATETIME NOT NULL,
    redeemed_at     DATETIME,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS password_reset_tokens_digest_idx
    ON password_reset_tokens (token_digest);

CREATE INDEX IF NOT EXISTS password_reset_tokens_user_idx
    ON password_reset_tokens (scope, belongs_to_user);

CREATE INDEX IF NOT EXISTS password_reset_tokens_expires_at_idx
    ON password_reset_tokens (expires_at);

