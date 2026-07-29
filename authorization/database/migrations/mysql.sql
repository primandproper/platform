CREATE TABLE IF NOT EXISTS {{PREFIX}}roles (
    id          VARCHAR(191) NOT NULL PRIMARY KEY,
    name        VARCHAR(191) NOT NULL,
    description TEXT,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at TIMESTAMP NULL,
    UNIQUE KEY {{PREFIX}}roles_name_idx (name)
);

CREATE TABLE IF NOT EXISTS {{PREFIX}}permissions (
    id          VARCHAR(191) NOT NULL PRIMARY KEY,
    name        VARCHAR(191) NOT NULL,
    description TEXT,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at TIMESTAMP NULL,
    UNIQUE KEY {{PREFIX}}permissions_name_idx (name)
);

CREATE TABLE IF NOT EXISTS {{PREFIX}}role_permissions (
    role_id       VARCHAR(191) NOT NULL,
    permission_id VARCHAR(191) NOT NULL,
    PRIMARY KEY (role_id, permission_id),
    KEY {{PREFIX}}role_permissions_permission_idx (permission_id),
    CONSTRAINT {{PREFIX}}role_permissions_role_fk
        FOREIGN KEY (role_id) REFERENCES {{PREFIX}}roles (id) ON DELETE CASCADE,
    CONSTRAINT {{PREFIX}}role_permissions_permission_fk
        FOREIGN KEY (permission_id) REFERENCES {{PREFIX}}permissions (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS {{PREFIX}}role_hierarchy (
    child_role_id  VARCHAR(191) NOT NULL,
    parent_role_id VARCHAR(191) NOT NULL,
    PRIMARY KEY (child_role_id, parent_role_id),
    KEY {{PREFIX}}role_hierarchy_parent_idx (parent_role_id),
    CONSTRAINT {{PREFIX}}role_hierarchy_child_fk
        FOREIGN KEY (child_role_id) REFERENCES {{PREFIX}}roles (id) ON DELETE CASCADE,
    CONSTRAINT {{PREFIX}}role_hierarchy_parent_fk
        FOREIGN KEY (parent_role_id) REFERENCES {{PREFIX}}roles (id) ON DELETE CASCADE,
    CONSTRAINT {{PREFIX}}role_hierarchy_no_self CHECK (child_role_id <> parent_role_id)
);
