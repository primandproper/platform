CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id              VARCHAR(64)  NOT NULL PRIMARY KEY,
    scope           VARCHAR(255) NOT NULL,
    belongs_to_user VARCHAR(255) NOT NULL,
    token_digest    VARCHAR(255) NOT NULL,
    expires_at      DATETIME(6)  NOT NULL,
    redeemed_at     DATETIME(6),
    created_at      DATETIME(6)  NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY password_reset_tokens_digest_idx (token_digest),
    KEY password_reset_tokens_user_idx (scope, belongs_to_user),
    KEY password_reset_tokens_expires_at_idx (expires_at)
);

