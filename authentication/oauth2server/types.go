package oauth2server

import (
	"maps"
	"slices"
	"time"
)

// Grant types this server implements. OAuth 2.1 removes the implicit and
// resource-owner-password grants, and this package does not bring them back —
// there is no option that re-enables them, because an option is a thing a
// deployment can be misconfigured into.
const (
	GrantTypeAuthorizationCode = "authorization_code"
	GrantTypeRefreshToken      = "refresh_token"
)

// ResponseTypeCode is the only response_type this server answers. It is the
// authorization code flow; everything else OAuth 2.0 defined returns a token
// through the front channel, which OAuth 2.1 removes.
const ResponseTypeCode = "code"

// Token endpoint authentication methods.
//
// AuthMethodNone is a public client — a CLI, a single-page app, anything that
// cannot hold a secret. It is not a weaker mode: PKCE is mandatory for every
// client here, so a public client's authorization code is bound to a verifier
// only the requester holds, and the secret would be adding a credential that
// ships in the binary.
const (
	AuthMethodNone         = "none"
	AuthMethodClientSecret = "client_secret_post"
	AuthMethodClientBasic  = "client_secret_basic"
)

// CodeChallengeMethodS256 is the only PKCE method this server accepts. The
// "plain" method puts the verifier in the authorization request, which is the
// request PKCE exists to protect, so supporting it would be supporting the
// attack.
const CodeChallengeMethodS256 = "S256"

// TokenTypeBearer is the token_type every token response carries.
const TokenTypeBearer = "Bearer"

// Subject is who a token is for, and is the seam where this package stops
// deciding.
//
// ID is the "sub": whatever the application calls a user, stable for as long
// as the tokens minted for it. Claims is everything else the resource server
// needs to act on the token — dinnerdonebetter's account identifier, a
// tenant, a role — and this package neither reads it nor constrains it beyond
// requiring it to be strings.
//
// Strings rather than `any`, deliberately. What goes in here comes back out of
// a token introspection or a JWT claim set, both of which are string-keyed
// JSON, and a map[string]any would let an application store something that
// round-trips through the database store as a different Go type than it went
// in as. Anything richer belongs behind the identifier in ID.
type Subject struct {
	// Claims is the application-shaped part of a token's identity. Nil is
	// fine; an empty map and a nil map mean the same thing.
	Claims map[string]string

	// ID is the "sub" claim. Required — a token with no subject authorizes
	// nobody, and the store rejects it.
	ID string
}

// Clone returns a deep copy, so that a record handed back by a store cannot be
// mutated through the caller's reference into the store's own state. The
// memory store depends on this; the database store gets it for free and calls
// it anyway, so the two cannot drift.
func (s Subject) Clone() Subject {
	out := Subject{ID: s.ID}
	if s.Claims != nil {
		out.Claims = maps.Clone(s.Claims)
	}

	return out
}

// Client is a registered OAuth client.
//
// Every field except Name and SecretHash is load-bearing at request time.
// RedirectURIs in particular: it is the field the map-backed examples store and
// never read again, and reading it is the check that decides whether an
// authorization code may be sent somewhere the client did not nominate.
type Client struct {
	// CreatedAt is when the registration was accepted.
	CreatedAt time.Time

	// ExpiresAt is when the registration lapses, or the zero time for a
	// registration that does not.
	//
	// Dynamic registration is open by construction — RFC 7591 requires it for
	// the discovery flow to work at all — so the table it writes to has an
	// anonymous writer. An expiry is what keeps that table bounded without
	// requiring anybody to decide which rows are garbage: a client that is
	// still in use re-registers, and one that is not ages out.
	ExpiresAt time.Time

	// ID is the client_id. Minted by the server from crypto/rand.
	ID string

	// SecretHash is the SHA-256 digest of the client secret, hex-encoded, or
	// empty for a public client.
	//
	// SHA-256 rather than argon2, and that is not a shortcut. A client secret
	// is 256 bits minted by this server, so there is no dictionary to attack
	// and nothing a work factor would buy; what it would cost is an argon2
	// verification on every single token request. Passwords are the opposite
	// case and go through authentication/argon2.
	SecretHash string

	// Name is the client_name from the registration request, shown on the
	// consent form. Cosmetic, and attacker-supplied — render it, never trust
	// it.
	Name string

	// TokenEndpointAuthMethod is how this client authenticates at /token: one
	// of AuthMethodNone, AuthMethodClientSecret, AuthMethodClientBasic.
	TokenEndpointAuthMethod string

	// RedirectURIs are the exact URIs this client may receive an authorization
	// code at. Matched exactly, byte for byte, as OAuth 2.1 requires: no
	// prefix matching, no wildcard, no ignored query string.
	RedirectURIs []string

	// GrantTypes and ResponseTypes are what the registration asked for,
	// narrowed to what this server implements.
	GrantTypes    []string
	ResponseTypes []string

	// Scopes are the scopes this client may request. An authorization request
	// for anything outside it is rejected rather than silently narrowed —
	// silently narrowing hands back a token that looks like the one that was
	// asked for and is not.
	Scopes []string
}

// Public reports whether this client holds no secret.
func (c *Client) Public() bool {
	return c == nil || c.SecretHash == ""
}

// Clone returns a deep copy. See Subject.Clone for why stores return copies.
func (c *Client) Clone() *Client {
	if c == nil {
		return nil
	}

	out := *c
	out.RedirectURIs = slices.Clone(c.RedirectURIs)
	out.GrantTypes = slices.Clone(c.GrantTypes)
	out.ResponseTypes = slices.Clone(c.ResponseTypes)
	out.Scopes = slices.Clone(c.Scopes)

	return &out
}

// AuthorizationCode is one issued authorization code.
//
// The code itself is not in here. What the store holds is Hash — the SHA-256
// digest of the value the client received — so that a dump of this table
// contains nothing that can be redeemed. That is a property a map-backed store
// gets for free by dying with the process and a table does not get at all.
type AuthorizationCode struct {
	IssuedAt  time.Time
	ExpiresAt time.Time

	// RedeemedAt is when the code was consumed, or the zero time. It is what
	// makes a second redemption detectable rather than merely unsuccessful.
	RedeemedAt time.Time

	// Hash is the hex-encoded SHA-256 digest of the code. It is the store's
	// primary key.
	Hash string

	ClientID string

	// FamilyID is the token family this code will mint, minted here at
	// /authorize rather than at the redemption that uses it.
	//
	// The timing is the whole point. RFC 6749 §4.1.2 says a code presented a
	// second time should revoke what the first presentation issued, and a
	// family decided at redemption is one a replay cannot name: the record
	// comes back with ErrAlreadyRedeemed carrying everything about the code
	// except the one field that says which tokens to revoke. Deciding it here
	// costs an identifier for a code that is never redeemed and makes the
	// replay actionable.
	FamilyID string

	// RedirectURI is the one the authorization request nominated, re-checked at
	// the token endpoint. A code issued for one URI cannot be redeemed by
	// naming another.
	RedirectURI string

	// CodeChallenge is the S256 challenge the code is bound to. Never empty —
	// PKCE is mandatory.
	CodeChallenge string

	// Nonce is echoed into the token record for an OpenID-shaped caller that
	// wants it. This package does not issue ID tokens and does not interpret
	// it.
	Nonce string

	Subject Subject

	Scopes []string

	// Resources are the RFC 8707 resource indicators the authorization request
	// asked for. They become the access token's audience, which is what stops
	// a token minted for one resource server being replayed at another.
	Resources []string
}

// Clone returns a deep copy. See Subject.Clone.
func (c *AuthorizationCode) Clone() *AuthorizationCode {
	if c == nil {
		return nil
	}

	out := *c
	out.Subject = c.Subject.Clone()
	out.Scopes = slices.Clone(c.Scopes)
	out.Resources = slices.Clone(c.Resources)

	return &out
}

// AccessToken is one issued access token.
//
// Opaque and stored, not signed and self-describing — see the package doc for
// why that is a decision rather than an accident, and what it costs. As with
// AuthorizationCode, what is stored is the digest.
type AccessToken struct {
	IssuedAt  time.Time
	ExpiresAt time.Time

	// RevokedAt is when the token was revoked, or the zero time. Revocation is
	// recorded rather than deleting the row so that a sweep and a revocation
	// are different events, and so that the row survives long enough for a
	// resource server's next request to be answered "no" rather than "never
	// heard of it".
	RevokedAt time.Time

	Hash     string
	ClientID string

	// FamilyID ties this token to the refresh-token family it was minted
	// under, so revoking the family revokes it.
	FamilyID string

	Subject Subject

	Scopes []string

	// Audience is the RFC 8707 resource this token is for. A resource server
	// that finds its own identifier absent must refuse the token: that check
	// is the reason resource indicators exist, and this package cannot make it
	// on the resource server's behalf.
	Audience []string
}

// Active reports whether the token is usable at now: issued, not revoked, not
// expired.
func (t *AccessToken) Active(now time.Time) bool {
	return t != nil && t.RevokedAt.IsZero() && now.Before(t.ExpiresAt)
}

// Clone returns a deep copy. See Subject.Clone.
func (t *AccessToken) Clone() *AccessToken {
	if t == nil {
		return nil
	}

	out := *t
	out.Subject = t.Subject.Clone()
	out.Scopes = slices.Clone(t.Scopes)
	out.Audience = slices.Clone(t.Audience)

	return &out
}

// RefreshToken is one issued refresh token.
//
// FamilyID is what makes rotation worth doing. Every refresh minted from a
// given authorization code carries the same family identifier, so when a
// redeemed token is presented a second time the server knows exactly which
// tokens to revoke: all of them. Rotation without that detects nothing — the
// replay is refused and the copy the attacker is actually using keeps working.
type RefreshToken struct {
	IssuedAt  time.Time
	ExpiresAt time.Time

	// RedeemedAt is when this token was exchanged, or the zero time. One-time
	// use: a second exchange is a replay.
	RedeemedAt time.Time

	// RevokedAt is when this token was revoked, whether individually through
	// /revoke or as part of its family.
	RevokedAt time.Time

	Hash     string
	ClientID string
	FamilyID string

	Subject Subject

	Scopes    []string
	Audience  []string
	Resources []string
}

// Clone returns a deep copy. See Subject.Clone.
func (t *RefreshToken) Clone() *RefreshToken {
	if t == nil {
		return nil
	}

	out := *t
	out.Subject = t.Subject.Clone()
	out.Scopes = slices.Clone(t.Scopes)
	out.Audience = slices.Clone(t.Audience)
	out.Resources = slices.Clone(t.Resources)

	return &out
}
