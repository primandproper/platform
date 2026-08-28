CREATE TABLE IF NOT EXISTS settings_definitions (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL,
    default_value   TEXT,
    admin_only      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS settings_definitions_name_uniq
    ON settings_definitions (scope, name);

CREATE INDEX IF NOT EXISTS settings_definitions_scope_idx
    ON settings_definitions (scope, id)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS settings_definition_options (
    definition_id TEXT NOT NULL REFERENCES settings_definitions (id) ON DELETE CASCADE,
    value         TEXT NOT NULL,
    PRIMARY KEY (definition_id, value)
);

CREATE TABLE IF NOT EXISTS settings_values (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    definition_id   TEXT NOT NULL REFERENCES settings_definitions (id) ON DELETE CASCADE,
    subject_type    TEXT NOT NULL,
    subject_id      TEXT NOT NULL,
    value           TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS settings_values_subject_uniq
    ON settings_values (scope, subject_type, subject_id, definition_id);

CREATE INDEX IF NOT EXISTS settings_values_definition_idx
    ON settings_values (scope, definition_id, id)
    WHERE archived_at IS NULL;

