/*
Package links mints URLs that prove their bearer was sent them, once, until they
expire.

Magic login, email verification, password reset, one-click unsubscribe: four
flows, one primitive. Every application writes it, and the four ways of writing
it wrong are always the same four — no expiry, a token that works twice, a token
sitting in the database in the clear, and a token nobody can withdraw once it is
loose.

	minter, err := links.NewMinter(store, locker,
		links.WithAction("magic_login", links.ActionPolicy{
			URL: "https://app.example.com/auth/magic/{token}",
			TTL: 15 * time.Minute,
		}),
	)

	link, err := minter.Mint(ctx, "magic_login", links.Subject(user.ID))
	// deliver link.URL; record link.ID

	claims, err := minter.Redeem(ctx, token)
	// claims.Subject is who to sign in; a second Redeem of that token fails

# What is stored

Not the token. The store is keyed by the SHA-256 digest of the token and holds
the action, the subject, the timestamps, and whatever metadata the minter
attached — nothing that can be turned back into a URL. A dump of the cache is a
list of links that were issued, not a set of live credentials, and that is the
difference between a leaked backup and a queue of account takeovers.

The digest is also the ID, which is what makes it useful rather than merely
safe. Mint returns it, Redeem returns it, Revoke takes it, and none of the three
has to write the token down to talk about the same link.

A fast unsalted hash is the right one here and a password KDF is not — see
WithHasher, which is where that goes wrong if anyone tries to improve it.

# Single use, and what enforces it

Redeem reads the record and writes it back consumed under a lock on that link.
Both halves are inside the lock, which is the whole of the guarantee: a check
that passes and a write that lands separately is exactly the window in which two
requests carrying one token both see it active.

That is why the locker is a required argument with no default. The noop locker
acquires unconditionally, and with it every test still passes — single use holds
for the sequential case and fails only under the concurrency an attacker
supplies deliberately.

Consumption is committed before the claims are returned. If the store cannot be
written, Redeem fails and hands back nothing, without a failure-policy knob.
idempotency offers FailOpen because a duplicate charge can cost less than an
outage; there is no version of that argument where the thing behind the link is
an account.

A redeemed link is kept rather than deleted, for WithRetention. That is what
lets a second attempt be told ErrLinkAlreadyRedeemed instead of
ErrLinkNotFound — two dead ends for the bearer, but only one of them is a
sentence a person can act on.

# Do not consume on GET

Redeem is not what a GET handler calls, and the reason is not theoretical.
Corporate mail security fetches every URL in every message before the recipient
sees it. A handler that consumes on GET has its link spent by a scanner, and the
user's own click — the first human one — arrives second and is refused.

	GET  /reset/{token}   Inspect, then render the form
	POST /reset           Redeem, then change the password

Inspect answers the same questions Redeem does without consuming anything, so
the page can say "this link expired" before asking for a new password. Its
answer is advisory: it takes no lock, so a link it approves can be spent by
somebody else a moment later. Nothing may be granted on Inspect alone, and
Redeem re-checks all of it.

Magic login has the same problem with no POST to hide behind, so it needs an
interstitial — a page that renders from Inspect and carries a button that
submits. A scanner does not press the button.

# Choosing a lifetime

There is no default TTL, and adding one would be a mistake rather than a
convenience. Fifteen minutes is right for a magic login and destroys an
unsubscribe link; a year is right for an unsubscribe link and leaves logins live
in mailboxes for a year. Every action states its own, next to its URL, because
the two are one decision — see ActionPolicy.

An action must be registered before it can be minted, which makes the registry
an allowlist as much as a configuration. A typo produces ErrUnknownAction rather
than a working-looking link to a page that does not exist.

# Delivering it

Email is the usual transport and this package does not know about it: hand
link.URL to whatever the email package is sending, or to qrcodes when the link
should be scannable rather than clicked. Nothing here composes with either at
the type level, because there is nothing to compose — the deliverable is a
string.

What matters at delivery is what happens either side of the URL. The token is in
it, so it is in the browser's history, in the Referer header of anything the
landing page loads from a third party, and in any access log that records query
strings. The mitigations are the landing page's, not this package's: send
Referrer-Policy: no-referrer, redirect to a clean URL immediately after
redeeming, and keep the lifetime short enough that a leaked URL is usually
already dead. Putting the token in the path rather than the query does not help
with Referer, and helps with access logs only until somebody logs full paths.

# Withdrawing one

Revoke takes an ID, not a token, because the server never had the token. The ID
is what Mint returned and what the audit entry for that mint should have
recorded, so a link can be withdrawn months later with nothing secret having
been kept in between.

"Invalidate every outstanding reset link for this account" is therefore a query
against the audit log — mints of that action for that subject, with no
corresponding redemption — followed by a Revoke per result. This package holds
no index to answer it directly, and building one would be a second, weaker copy
of a log the application already keeps.

# Recording it

Mint and redeem are exactly what audit exists for, and this package does not
write those entries itself. The entry belongs in the same transaction as the
effect — the session that was created, the password that was changed — and this
package does not own that transaction. What it provides is the part that is
awkward to get right otherwise: an ID that identifies the link in both entries
and discloses nothing.

Log the ID. Never the token, never the URL. Nothing in this package writes
either to a span, a log line, or a metric attribute, and the type carries no
redacting String method to enforce it — one that silently produced a broken URL
from an ordinary string concatenation would cost more than it saved.

# Deliberately not a JWT

Nothing in the URL is readable, so nothing in it can be trusted by mistake.
There is no algorithm to confuse, no key to rotate on a deadline, no claim a
holder can assert, and no way to be handed one that is valid but stale. Claims
are looked up rather than parsed.

The cost is real and worth naming: redemption requires the store, so links stop
working when it does. For account recovery that is the correct trade — a
credential that cannot be revoked is worse than one that occasionally cannot be
used — and it is not the correct trade everywhere. A stateless token that
carries its own claims is a different thing, and this package is not a slow
version of it.

# The store

The store is a cache.Cache[Record], and the redis provider is the production
answer. The memory provider is per-process, so a link minted by one replica does
not exist for the next; it also needs cache/memory's WithJanitor to reclaim
anything, since an entry written once and never read again is never lazily
evicted.

Record expiry is decided by this package against its own clock, not by the
cache. The store's TTL is deliberately set past the link's, so a cache that
evicts late — or not at all — cannot keep a credential alive past the moment it
was supposed to die.

Every record carries a Version. A record written by a different shape reads as
absent rather than being decoded with the wrong field meanings, so changing the
shape of Record invalidates outstanding links. That is the safe direction, and
it is a deploy concern: bump recordVersion when Record changes, and expect the
links in flight at that moment to stop working.

# Watching it

	links_minted         by action.
	links_redemptions    by action and outcome: redeemed, not_found,
	                     already_redeemed, expired, revoked, invalid_token,
	                     store_error.
	links_revocations    by action.
	links_store_errors   store health. Every one of these is a redemption that
	                     did not happen — the alert.
	links_stale_records  records ignored for carrying another version; expected
	                     to spike once after a shape change and then return to
	                     zero.
	links_latency_ms     per operation.

already_redeemed is the row to watch and the one most often misread. A steady
low rate is people clicking twice. A rate that tracks minting is a mail scanner
consuming links on GET, which means a handler is redeeming where it should be
inspecting. A spike against one subject is somebody else's mailbox.

No metric is labeled by subject. Nothing bounds that cardinality, and the
question it would answer is the audit log's.
*/
package links
