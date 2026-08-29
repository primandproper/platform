/*
Package authentication holds the password engine and names the boundary the
sign-in flow sits on the other side of.

[Authenticator] hashes a password and compares one against a stored hash, and
[github.com/primandproper/platform-go/v13/authentication/argon2] is the
implementation this module recommends and the only one it ships. Every other
piece a sign-in touches is a package beside this one:
[github.com/primandproper/platform-go/v13/authentication/totp] verifies the
second factor, [github.com/primandproper/platform-go/v13/authentication/webauthn]
is the relying party a passkey answers to,
[github.com/primandproper/platform-go/v13/authentication/passwordreset] holds the
token for somebody who can present neither,
[github.com/primandproper/platform-go/v13/authentication/tokens] mints the bearer
credential a proven identity is exchanged for,
[github.com/primandproper/platform-go/v13/authentication/oauth2server] is the
authorization server, [github.com/primandproper/platform-go/v13/identity]'s
SignInReader is the read a submitted handle resolves through, and
[github.com/primandproper/platform-go/v13/sessions] is what a proven identity
becomes.

What no package here ships is the function that calls them in order. That is a
decision rather than a gap, and this is the ruling.

# The flow is the consumer's

The case for shipping a login manager is the case that produced passwordreset:
security-critical boilerplate, short enough to look like it needs no library and
dangerous enough that its mistakes are vulnerabilities rather than bugs. Four of
them are named below, and every one of them can be got wrong twice.

The case against is what a manager would have to take to encode those four. It
needs a directory reader, a password engine, a set of second factors and a
policy deciding which of them this user must present now, a session store, a
principal assembler, an event sink, a refusal vocabulary, and a limiter with a
key function. That is eight seams to hold four conditionals, and each of the
eight is somewhere a consumer can wire it wrong in a way that reintroduces
exactly what the manager existed to prevent. A WithMFAPolicy hook that answers
"not required" for a user holding a verified secret is the second-factor bypass,
written in the manager's own vocabulary and now harder to see.

The difference from passwordreset is what the guarantee is made of. What that
package ships is a table and three statements: a digest in the column, single use
as the affected-row count of a guarded UPDATE inside one transaction, an expiry
refused on read. Those hold however the caller sequences its code, and a consumer
who wires them wrong gets an error rather than a silent bypass, because the
correctness is inside the statement. The four mistakes below are properties of
control flow — of what runs before what, and of what is minted before which check
— and a seam cannot hold one of those on a caller's behalf. It can only offer to
run the caller's code in the right order, which is the thing the caller was going
to write anyway.

So: orchestration is the consumer's. What this module owes instead is engines
that fail safe on their own, and this document, which names the mistakes and
points at an example that makes them executable.

This is a ruling and not a law. What would overturn it is evidence rather than
appetite: two consumers whose flows differ only in the event vocabulary and the
principal's shape are a shape written twice rather than a policy written twice,
and that is the case a manager would be built from. Example_loginFlow is what it
would have to be specified against.

# The order, and what each step's mistake costs

Rate-limit before reading anything.
[github.com/primandproper/platform-go/v13/ratelimiting] is what this module ships
for that, and it is a limiter rather than a lockout: nothing here counts a user's
failures or freezes an account after N of them. That is left out because a
per-account lockout is a denial of service anybody can aim at somebody else by
guessing their handle badly on purpose, and whether to accept that — or to key
the budget on the source, or on both — is a policy with no single right answer.
Pick the key deliberately; there is no default that is safe everywhere.

Read the user, and spend a comparison whether or not there was one. A handle that
resolves to nothing and a handle that resolves to a user with the wrong password
have to cost the same and say the same. Returning early on the miss makes the
response time the answer: an argon2id verification at 64 MiB is the most
expensive thing on this path, and skipping it leaves a gap nobody needs
statistics to read. Compare against a fixed decoy hash minted at startup, and
refuse both with one error.

Check the password before the status.
[github.com/primandproper/platform-go/v13/identity.AccountStatus.AdmitsSignIn] is
the rule for who may authenticate, and it is asked after the comparison, never
before. A ban tested first tells anyone who can guess a username that the account
exists and that its owner is suspended — two facts about somebody else, handed
out for free. Tested afterwards, learning them costs the password, and whoever
paid it is the account's owner, for whom the explanation on the status was
written.

Gate the second factor, and mint nothing before it. A password that verified is
not a sign-in. What says a user holds a second factor is
[github.com/primandproper/platform-go/v13/identity.User.TwoFactorSecretVerifiedAt]
rather than a non-empty secret — a secret issued and never proven is a QR code
somebody may have closed — and when it is set and no code arrived, the answer is
that a code is required and nothing else: no session, no token, no cookie, no
value the client can present next time in place of the code. The bypass this
forbids is rarely a missing check. It is a check that runs after something was
already minted "for the second step". Whatever carries the request from the first
step to the second is itself a credential, and wants the treatment one gets:
short-lived, single-use, and bound to the account it was minted for.

The passkey branch is shorter, and it drops exactly two of these. There is no
password, so there is no decoy comparison and no second factor to gate — the
assertion is both. Everything else survives.
[github.com/primandproper/platform-go/v13/authentication/webauthn.RelyingParty.BeginLogin]
names a user and therefore answers with that user's credential IDs, which tells
whoever asked that the handle exists and how many keys are on it;
BeginDiscoverableLogin names nobody, and it is what a sign-in page open to the
world should call. The status check still lands after the assertion verifies,
for the reason it lands after the comparison. The identifier is still fresh. And
FinishLogin hands back a credential carrying the authenticator's sign count,
which is evidence of a cloned key only if the last one was written back — a step
with no analog on the password path and no default that supplies it.

Establish a new session identifier, every time.
[github.com/primandproper/platform-go/v13/sessions.Store.NewFor] mints one and
records who holds it; anything the client was carrying before must stop
resolving. sessions' documentation has the long form under "Renewal is not
optional" — an identifier planted in a victim's browser before sign-in and still
valid after it is session fixation, and it is a defect in the flow rather than in
the cookie.

Record the outcome, refusals included. The event vocabulary is the consumer's —
[github.com/primandproper/platform-go/v13/audit] for the tamper-evident trail,
[github.com/primandproper/platform-go/v13/eventstream] for what other services
react to — but the shape of the mistake is not: a sign-in recorded before the
session exists records sign-ins that did not happen, and a refusal that records
nothing is how a stuffing run stays invisible. A password that worked followed by
a code that did not is the most interesting event this flow produces, which is
why totp's verifier already records its own rejections.

# Mismatch is (false, nil), and the sentinel is yours

[Authenticator.PasswordMatches] reports a wrong password as (false, nil) and
populates err only when the comparison could not be performed — a malformed
stored hash, a runtime failure. That is the shape
[github.com/primandproper/platform-go/v13/ratelimiting.RateLimiter] uses for a
refusal, for the same reason: the caller is deciding what to do next rather than
propagating a failure, and a refusal delivered as an error is one that gets
logged at error level, alerted on, and retried.

The module ships no mismatch sentinel and recommends the consumer's own, because
of what that sentinel has to cover. A flow hands its transport one error meaning
"these credentials sign nobody in", and that one error has to answer for the
unknown handle, the wrong password, and the wrong second-factor code alike —
three refusals from three packages, of which a sentinel declared here could speak
for exactly one. Owning it here would make the other two the ones a caller
forgot to translate. Declare it beside the flow, map it in
[github.com/primandproper/platform-go/v13/errors/http], and convert where the
boolean is read:

	matched, err := authenticator.PasswordMatches(ctx, user.HashedPassword, password)
	if err != nil {
		return nil, err // the stored hash is broken; that is not a wrong password
	}

	if !matched {
		return nil, errBadCredentials // yours, and it covers the other refusals too
	}

A caller that treats any error as a failed sign-in is correct. One that treats a
failed sign-in as an error is not.

# The principal a session carries is opaque, and that is a boundary

[github.com/primandproper/platform-go/v13/sessions.Holder]'s principal is a
string, and Store is generic over whatever the application puts in the record.
Neither knows what a user is. identity has a Principal of its own — the user,
their memberships, and the account the request is against — and it is
deliberately not what sessions stores.

A consumer's principal is usually fatter still: memberships plus a permission map
resolved from roles, which is a snapshot of an authorization decision. Storing
that in the session record makes the session a cache of policy, and a role
revoked while somebody is signed in does not take effect until they sign in
again. Storing only the user ID and calling identity's GetPrincipal per request
costs four statements on every authenticated request, which that method's
documentation says out loud so the trade can be made with the number in hand.
Neither answer is wrong, which is precisely why the record's shape belongs to the
consumer. What this module will not do is settle it by shipping a principal type
that sessions stores and authorization reads.

One constraint the shape does have to respect: sessions.Record carries a version,
and a record written under a different shape reads as absent rather than being
decoded into the current one. Widening a principal is a wave of re-logins, so it
is worth deciding once whether the permissions go in.

# Impersonation is not a platform notion

Nothing in this module has one, and that is worth saying plainly because the
absence is easy to paper over. A support engineer acting as a user is two
identities — the actor and the subject — and every layer beneath this one has
room for exactly one: a session holder is one principal, identity's Principal is
one user with their memberships, authorization resolves one subject's
permissions, and an [github.com/primandproper/platform-go/v13/audit.Entry] has
one Actor.

The papering-over is to put the subject's ID where the actor's belongs. That
produces a system which works and an audit trail which says the user did it,
discovered during the incident it was supposed to explain. A shape that does not
lie carries both — the actor in the session's own record, the subject as a field
the flow set explicitly — and every read answering "who is this" has to say which
of the two it means. audit's Entry can carry the second in its Metadata today,
which is a place to put it rather than a model of it.

Making it a platform notion means a second principal on the session record, a
second subject through authorization, and an actor/subject pair in audit: a
change to three packages rather than a helper in this one. It stays out until a
consumer needs it enough to specify it, and a consumer that needs it now should
model it explicitly rather than by substitution.

# The worked example

Example_loginFlow is the order above, executable: an identity store over SQLite,
argon2, totp, and a session store, wired into the function this package declines
to ship. It is a test rather than prose so that what it demonstrates is
checked — the enumeration parity, the status check landing after the comparison,
the required-code answer minting nothing, and the identifier changing across a
re-login are assertions in the file beside it.
*/
package authentication
