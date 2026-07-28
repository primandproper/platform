CREATE TABLE IF NOT EXISTS {{PREFIX}}roles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}roles_name_idx ON {{PREFIX}}roles (name);

CREATE TABLE IF NOT EXISTS {{PREFIX}}permissions (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}permissions_name_idx ON {{PREFIX}}permissions (name);

CREATE TABLE IF NOT EXISTS {{PREFIX}}role_permissions (
    role_id       TEXT NOT NULL REFERENCES {{PREFIX}}roles (id) ON DELETE CASCADE,
    permission_id TEXT NOT NULL REFERENCES {{PREFIX}}permissions (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}role_permissions_permission_idx
    ON {{PREFIX}}role_permissions (permission_id);

CREATE TABLE IF NOT EXISTS {{PREFIX}}role_hierarchy (
    child_role_id  TEXT NOT NULL REFERENCES {{PREFIX}}roles (id) ON DELETE CASCADE,
    parent_role_id TEXT NOT NULL REFERENCES {{PREFIX}}roles (id) ON DELETE CASCADE,
    PRIMARY KEY (child_role_id, parent_role_id),
    CHECK (child_role_id <> parent_role_id)
);

CREATE INDEX IF NOT EXISTS {{PREFIX}}role_hierarchy_parent_idx
    ON {{PREFIX}}role_hierarchy (parent_role_id);
