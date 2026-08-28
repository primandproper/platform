-- scope is whose settings these are: a reseller, a region, a product, or — as
-- the empty string — nobody. Every read of every table here filters on it.
--
-- It has no default. The empty string is a scope, tenancy.Global(), and a
-- column that supplied it for a write which did not name one would hand out the
-- global scope to whoever forgot the column — the mistake tenancy.Scope exists
-- to make unspellable in Go. NOT NULL with nothing to fall back on makes that
-- write fail instead. See the tenancy package.
--
-- A definition and the values stored against it share a scope. A deployment
-- with one catalog leaves every row global and gets exactly the behavior it
-- would have had without the column; a deployment whose tenants each define
-- their own settings gives each tenant's rows that tenant's scope. What the
-- schema does not offer is a global definition with per-tenant values, because
-- resolving one would take two scopes into a read whose whole guarantee is that
-- it takes one. See the settings package documentation.
CREATE TABLE IF NOT EXISTS {{PREFIX}}settings_definitions (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    kind            TEXT NOT NULL,
    default_value   TEXT,
    admin_only      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

-- default_value is the one nullable column in this schema that could have been
-- NOT NULL DEFAULT '', and making it nullable is the whole of what "absence is
-- distinguishable from zero" means at the storage layer. A string setting whose
-- default is the empty string and a string setting with no default at all are
-- different definitions: the first answers every subject that has not chosen,
-- and the second answers none of them. Collapsed into one column value they
-- would be the same row.

-- Names are unique per scope, and the uniqueness covers archived rows as well
-- as live ones — see the Postgres schema for why a soft delete does not free a
-- setting's name.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}settings_definitions_name_uniq
    ON {{PREFIX}}settings_definitions (scope, name);

-- Serves the catalog page: one scope, live rows, walked by id.
CREATE INDEX IF NOT EXISTS {{PREFIX}}settings_definitions_scope_idx
    ON {{PREFIX}}settings_definitions (scope, id)
    WHERE archived_at IS NULL;

-- The values a definition admits, one row each. An empty set means the
-- definition is not enumerated and any value of its kind is legal. A set rather
-- than a sequence — see the Postgres schema.
CREATE TABLE IF NOT EXISTS {{PREFIX}}settings_definition_options (
    definition_id TEXT NOT NULL REFERENCES {{PREFIX}}settings_definitions (id) ON DELETE CASCADE,
    value         TEXT NOT NULL,
    PRIMARY KEY (definition_id, value)
);

-- A subject's answer to one definition. subject_type and subject_id are two
-- columns rather than one composite string — see the Postgres schema.
CREATE TABLE IF NOT EXISTS {{PREFIX}}settings_values (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    definition_id   TEXT NOT NULL REFERENCES {{PREFIX}}settings_definitions (id) ON DELETE CASCADE,
    subject_type    TEXT NOT NULL,
    subject_id      TEXT NOT NULL,
    value           TEXT NOT NULL,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_updated_at DATETIME,
    archived_at     DATETIME
);

-- One row per subject per definition, live and archived alike — which is what
-- makes setting a value that was previously cleared revive the row rather than
-- write a second one.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}settings_values_subject_uniq
    ON {{PREFIX}}settings_values (scope, subject_type, subject_id, definition_id);

-- Serves the read that answers "who has overridden this setting", which is also
-- the walk an edit to a definition's kind or enumeration is checked against.
CREATE INDEX IF NOT EXISTS {{PREFIX}}settings_values_definition_idx
    ON {{PREFIX}}settings_values (scope, definition_id, id)
    WHERE archived_at IS NULL;
