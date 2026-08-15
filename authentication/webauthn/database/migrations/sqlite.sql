CREATE TABLE IF NOT EXISTS {{PREFIX}}webauthn_sessions (
    challenge    TEXT PRIMARY KEY,
    session_data BLOB NOT NULL,
    expires_at   DATETIME NOT NULL
);

-- Serves the sweeper and Consume's expiry check. See postgres.sql.
CREATE INDEX IF NOT EXISTS {{PREFIX}}webauthn_sessions_expires_at_idx
    ON {{PREFIX}}webauthn_sessions (expires_at);
