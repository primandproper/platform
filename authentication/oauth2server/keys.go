package oauth2server

import (
	"net/http"
	"slices"
	"strings"
)

// Observability keys for this package's spans and log fields.
//
// Note what is absent: every credential. No authorization code, access token,
// refresh token, client secret, code verifier, or challenge appears on a span
// or in a log line — not even hashed, since a hash of a bearer credential is a
// lookup key for the store that holds it. What is recorded identifies a request
// without carrying anything that could replay it.
//
// The client identifier is recorded, and is the one judgement call here. It is
// public by construction — it travels in a query string on every authorization
// request — and without it a spike in invalid_grant cannot be traced to the
// client causing it.
const (
	endpointKey  = "oauth2server.endpoint"
	errorCodeKey = "oauth2server.error_code"
	clientIDKey  = "oauth2server.client_id"
	grantTypeKey = "oauth2server.grant_type"
	familyIDKey  = "oauth2server.family_id"
	scopeKey     = "oauth2server.scope"
	revokedKey   = "oauth2server.revoked_records"
)

// Endpoint names, reported as an attribute on every instrument so that one
// latency histogram and one error counter serve all five.
const (
	endpointMetadata  = "metadata"
	endpointAuthorize = "authorize"
	endpointToken     = "token"
	endpointRegister  = "register"
	endpointRevoke    = "revoke"
)

// The OAuth request parameters this package reads, spelled once.
const (
	paramClientID            = "client_id"
	paramClientSecret        = "client_secret"
	paramRedirectURI         = "redirect_uri"
	paramResponseType        = "response_type"
	paramScope               = "scope"
	paramState               = "state"
	paramNonce               = "nonce"
	paramResource            = "resource"
	paramCodeChallenge       = "code_challenge"
	paramCodeChallengeMethod = "code_challenge_method"
	paramCodeVerifier        = "code_verifier"
	paramCode                = "code"
	paramGrantType           = "grant_type"
	paramRefreshToken        = "refresh_token"
	paramToken               = "token"
	paramTokenTypeHint       = "token_type_hint"
	paramError               = "error"
	paramErrorDescription    = "error_description"
	paramIss                 = "iss"
)

// Token type hints from RFC 7009 §2.1.
const (
	hintAccessToken  = "access_token"
	hintRefreshToken = "refresh_token"
)

// splitScopes parses a space-delimited scope string, dropping empties and
// duplicates.
//
// Space-delimited and not comma-delimited: RFC 6749 §3.3 says space, and a
// server that also accepted commas would issue tokens whose scope string means
// something different to it than to a client that read the spec.
func splitScopes(scope string) []string {
	fields := strings.Fields(scope)
	if len(fields) == 0 {
		return nil
	}

	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}

	return out
}

// joinScopes renders scopes back onto the wire.
func joinScopes(scopes []string) string {
	return strings.Join(scopes, " ")
}

// writeMetadata writes a discovery document.
func writeMetadata(res http.ResponseWriter, doc any) {
	res.Header().Set("Content-Type", "application/json")
	res.WriteHeader(http.StatusOK)

	writeBody(res, doc)
}
