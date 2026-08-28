CREATE TABLE IF NOT EXISTS sessions (
    id           VARCHAR(255) NOT NULL PRIMARY KEY,
    data         LONGBLOB     NULL,
    created_at   DATETIME(6)  NOT NULL,
    last_seen_at DATETIME(6)  NOT NULL,
    expires_at   DATETIME(6)  NOT NULL,
    version      INT          NOT NULL,
    KEY sessions_expires_at_idx (expires_at)
);

