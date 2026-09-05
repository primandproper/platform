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
-- There is no scope column, and its absence is the one departure from this
-- module's tenancy rule. A link is not read by enumeration and never by
-- anything but the bearer's own digest, and links.Mint takes no scope to bind —
-- adding the column means changing that signature, which is a decision of its
-- own rather than a consequence of moving the records into a table.
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
