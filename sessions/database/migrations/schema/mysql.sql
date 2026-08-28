CREATE TABLE IF NOT EXISTS sessions (
    id           VARCHAR(255) NOT NULL PRIMARY KEY,
    scope        VARCHAR(255) NOT NULL,
    principal    VARCHAR(255) NOT NULL,
    data         LONGBLOB     NULL,
    device_name  VARCHAR(255) NOT NULL,
    ip_address   VARCHAR(255) NOT NULL,
    user_agent   TEXT         NOT NULL,
    login_method VARCHAR(255) NOT NULL,
    created_at   DATETIME(6)  NOT NULL,
    last_seen_at DATETIME(6)  NOT NULL,
    expires_at   DATETIME(6)  NOT NULL,
    version      INT          NOT NULL,
    KEY sessions_expires_at_idx (expires_at),
    KEY sessions_principal_idx (scope, principal, created_at)
);

