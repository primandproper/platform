-- Two tables, and the split is the difference between a fact about a person and
-- a fact about a handset.
--
-- The inbox is what somebody was told and whether they have read it. It is a
-- conventional row: created, soft-deleted, listed and filtered like every other
-- consumer row in this module, and the read stamp is a fourth timestamp beside
-- the triple rather than a status column, because "when" answers "whether" and a
-- status does not.
--
-- The registry is what a push is addressed to, and it is the half that goes
-- wrong quietly. A provider reports an invalid or expired token on send, and a
-- registry that never prunes them keeps pushing into the void while reporting
-- success — so an invalidated token is deleted rather than archived, and the row
-- carries no archived_at for a delivery path to forget to filter on.
--
-- scope is whose data this row is: a reseller, a region, a product, or — as the
-- empty string — nobody. Every read of either table filters on it. It has no
-- default in either table, and that is deliberate: the empty string is a scope,
-- tenancy.Global(), and a column that supplies it for a write which did not name
-- one hands the global scope to whoever forgot the column. NOT NULL with nothing
-- to fall back on makes that write fail instead. See the tenancy package.
--
-- principal is whose inbox and whose handset — a user id in every deployment
-- this module has, but a string here rather than a reference, because
-- notifications does not own the directory and a consumer storing its people
-- somewhere else still has an inbox.
CREATE TABLE IF NOT EXISTS {{PREFIX}}notifications_inbox (
    id              TEXT PRIMARY KEY,
    scope           TEXT NOT NULL,
    principal       TEXT NOT NULL,
    -- topic is the application's own category for the notification —
    -- order.shipped, invite.received — and it is what a client groups, mutes or
    -- routes by. Not validated here: the catalog of topics is the consumer's.
    topic           TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL DEFAULT '',
    -- link is where reading the notification takes somebody, as the application
    -- spells it. Empty is a notification with nowhere to go, which is the
    -- ordinary case for one that is only an announcement.
    link            TEXT NOT NULL DEFAULT '',
    -- read_at is NULL until somebody reads it, which is this module's spelling
    -- of "has not happened yet" and what the unread list keys on. A boolean
    -- would answer whether without answering when, and the when is what a
    -- digest, a re-notify and a support conversation are all about.
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_updated_at TIMESTAMPTZ,
    archived_at     TIMESTAMPTZ
);

-- Serves the inbox page: one person's notifications within one scope, walked by
-- the cursor's id. Partial on the soft delete, so the index tracks the live
-- inbox rather than every notification ever sent — this table grows with
-- traffic and is read on every app open.
CREATE INDEX IF NOT EXISTS {{PREFIX}}notifications_inbox_principal_idx
    ON {{PREFIX}}notifications_inbox (scope, principal, id)
    WHERE archived_at IS NULL;

-- Serves the unread page and the count that rides on it, which is the badge
-- number every client asks for first. Partial on both predicates: an inbox that
-- is mostly read has an unread set of a handful of rows, and this is the index
-- that stays that size.
CREATE INDEX IF NOT EXISTS {{PREFIX}}notifications_inbox_unread_idx
    ON {{PREFIX}}notifications_inbox (scope, principal, id)
    WHERE archived_at IS NULL AND read_at IS NULL;

-- The device registry. No convention triple beyond created_at, and the two
-- missing columns are the decision: a token is revoked by its owner or
-- invalidated by the provider, and either way the row goes. archived_at would
-- leave rows every send path has to remember to exclude, and the one that
-- forgot would push into the void forever while reporting success.
--
-- last_seen_at is not last_updated_at wearing another name. It is the last time
-- the device announced itself, which is what a stale-token sweep reads and what
-- decides whether a handset that has not opened the app in a year is still worth
-- pushing to; nothing else about the row is mutable.
CREATE TABLE IF NOT EXISTS {{PREFIX}}notifications_devices (
    id           TEXT PRIMARY KEY,
    scope        TEXT NOT NULL,
    principal    TEXT NOT NULL,
    -- platform is which provider the token is addressed through: ios for APNs,
    -- android for FCM. It is half the natural key, because the two providers
    -- mint their tokens independently and neither namespace excludes the other.
    platform     TEXT NOT NULL,
    token        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per live token, across every scope and principal, and the uniqueness
-- is what makes re-registration a move rather than a fan-out. A handset that
-- one person signs out of and another signs into presents the same token under
-- a new principal: converging on the token reassigns the row, while a key that
-- included the principal would leave two rows and push the second person's
-- notifications to the first person's phone.
--
-- It is also the conflict target the registration upsert names, so it has to be
-- exactly these columns — Postgres matches ON CONFLICT against a unique index
-- the table actually has.
CREATE UNIQUE INDEX IF NOT EXISTS {{PREFIX}}notifications_devices_token_uniq
    ON {{PREFIX}}notifications_devices (platform, token);

-- Serves both reads the registry answers: one person's devices, walked by the
-- cursor's id, and the batched fan-out over a set of principals.
CREATE INDEX IF NOT EXISTS {{PREFIX}}notifications_devices_principal_idx
    ON {{PREFIX}}notifications_devices (scope, principal, id);
