-- No convention triple, and that is deliberate. An action link is minted,
-- resolved once, and collected; archived_at would keep rows nothing can ever
-- read, and last_updated_at would be a second copy of resolved_at, since
-- resolution is the only mutation this row has. What reclaims the table is the
-- sweeper, on purge_after.
--
-- id is the digest of the token and never the token. The token is the whole
-- credential — whoever holds it can redeem the link — so a database copy is a
-- queue of account takeovers if this column holds the raw value. It is not
-- salted, and does not need to be, because what it digests is thirty-two bytes
-- of randomness rather than something a person chose; there is no dictionary to
-- run against it. It is also why the digest is the primary key rather than a
-- column beside a surrogate id: a link has exactly one name, and a second one
-- would be a second thing to look a redemption up by.
--
-- There is no scope column, and that is a decision rather than a deferral.
-- This module's tenancy rule exists to stop a read that forgot the scope from
-- matching everything, and that failure needs a read which can widen. There is
-- none here. Every statement is keyed by this primary key, and this primary key
-- is a digest of thirty-two bytes of randomness, so the only read this table
-- has is "the one row named by a value nobody can guess".
--
-- The column would not earn its place on the redemption path either. Whoever
-- holds the token holds the credential, so a scope predicate refuses nobody it
-- did not already refuse; what it adds is a second fact the redeemer must know
-- first, and a magic-login link exists precisely to identify a caller who is
-- not yet known and whose tenant therefore is not known either.
--
-- The reads that would enumerate want the subject instead. Revoking every live
-- link for a person should cross whatever tenants that person belongs to rather
-- than stop inside one, and erasure is subject-keyed throughout this module —
-- dataprivacy.Eraser takes a subject and lets each component resolve its own
-- scopes from it. subject is already a column here.
--
-- What this table holds is a credential and not a domain record: named by its
-- own secret, legible only to the bearer, and collected within the retention
-- window. The tenancy rule governs domain records. An application that wants a
-- tenant recorded against a link it minted has metadata for it, and wants the
-- invitation itself in its own schema regardless.
--
-- action and subject are what the link is bound to: which flow it belongs to,
-- and who it is for. The action is the half that stops a verification link
-- redeeming as a login, so it is stored rather than inferred at redemption.
-- subject is an opaque identifier this table cannot resolve and carries no
-- REFERENCES: an application keeping its users in another schema, another
-- service, or a table predating this module still gets to use action links.
--
-- metadata is what the minter attached, encoded by the store's codec. It is
-- nullable because most links carry none, and it is in the clear — which is the
-- one thing the token deliberately is not.
--
-- state is what has happened to the link, as links.State: 1 active, 2 redeemed,
-- 3 revoked. resolved_at is NULL for exactly the rows whose state is active,
-- and it rather than state is what the resolution guards on — "has not happened
-- yet" is a value a statement can compare against, where "is still 1" is one a
-- caller has to have read first.
--
-- version is the record shape the row was written with. A row carrying another
-- one reads as absent rather than being decoded with the wrong field meanings,
-- so a deploy that changes links.Record invalidates the links in flight at that
-- moment. That is the safe direction for a credential.
--
-- expires_at is when the link stops being redeemable and purge_after is when
-- this row may be deleted, which is later by the minter's retention window.
-- The two are separate columns because they answer different questions: the
-- first decides redemption, the second buys the difference between "that link
-- was already used" and "no such link".
CREATE TABLE IF NOT EXISTS {{PREFIX}}action_links (
    id          TEXT PRIMARY KEY,
    action      TEXT NOT NULL,
    subject     TEXT NOT NULL,
    metadata    BYTEA,
    state       INTEGER NOT NULL,
    version     INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    resolved_at TIMESTAMPTZ,
    purge_after TIMESTAMPTZ NOT NULL
);

-- Serves the sweeper, which is the only statement in this package that reads
-- rows it cannot name. Every other one is keyed by the primary key.
--
-- purge_after is written by the minter and read by nothing else: whether a link
-- is redeemable is decided from expires_at against the minter's clock, so a row
-- this index has not helped collect yet is already refused. The sweep is what
-- keeps the table from growing by a row per link ever minted, not what makes a
-- link stop working.
CREATE INDEX IF NOT EXISTS {{PREFIX}}action_links_purge_after_idx
    ON {{PREFIX}}action_links (purge_after);

-- Serves the plural revoke: every link a subject still has outstanding, moved
-- in one statement, for a completed password reset, a locked account, or an
-- erasure. It is the other read in this package that cannot name its rows in
-- advance, and it is why the subject column earns an index that the redemption
-- path would never have asked for.
--
-- resolved_at is the second column rather than a WHERE on a partial index.
-- Postgres and SQLite would take `WHERE resolved_at IS NULL` and MySQL has no
-- such thing, so a partial index here would be a third spelling of one index
-- across three files — the drift this schema spends its comments avoiding — to
-- save a column on a table whose rows are collected within the retention
-- window. The composite serves `subject = ? AND resolved_at IS NULL` on all
-- three engines.
--
-- The pair is deliberately not (subject, action). An operator revoking after a
-- suspected compromise does not know what was minted, which is the whole reason
-- the statement exists.
CREATE INDEX IF NOT EXISTS {{PREFIX}}action_links_subject_idx
    ON {{PREFIX}}action_links (subject, resolved_at);
