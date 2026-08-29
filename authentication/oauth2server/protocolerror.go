package oauth2server

import (
	"encoding/json"
	"net/http"
)

// The RFC 6749 §5.2 / §4.1.2.1 error codes, plus the two later ones this
// server can emit. They are the strings a client branches on, so they are
// constants rather than literals scattered across four handlers.
const (
	ErrorCodeInvalidRequest          = "invalid_request"
	ErrorCodeInvalidClient           = "invalid_client"
	ErrorCodeInvalidGrant            = "invalid_grant"
	ErrorCodeUnauthorizedClient      = "unauthorized_client"
	ErrorCodeUnsupportedGrantType    = "unsupported_grant_type"
	ErrorCodeUnsupportedResponseType = "unsupported_response_type"
	ErrorCodeInvalidScope            = "invalid_scope"
	ErrorCodeAccessDenied            = "access_denied"
	ErrorCodeServerError             = "server_error"

	// ErrorCodeInvalidTarget is RFC 8707 §2: a resource indicator this server
	// does not mint tokens for.
	ErrorCodeInvalidTarget = "invalid_target"

	// The RFC 6750 §3.1 resource server errors. They are what a protected
	// resource sends in a WWW-Authenticate challenge rather than in a body, and
	// they are the two codes this package's Verifier emits.
	ErrorCodeInvalidToken      = "invalid_token"
	ErrorCodeInsufficientScope = "insufficient_scope"

	// The RFC 7591 §3.2.2 registration errors.
	ErrorCodeInvalidRedirectURI    = "invalid_redirect_uri"
	ErrorCodeInvalidClientMetadata = "invalid_client_metadata"
)

// protocolError is one OAuth error response: a code from the registry, a
// description safe to show a client, and the status to send it at.
//
// The description is deliberately separate from the underlying error's message.
// What goes on the wire is written for the client's author; what goes in the
// operation record is the wrapped cause, which is usually more specific than
// anything that should leave the process. Answering "invalid_grant" to a
// replayed refresh token while logging "refresh token reuse detected, family
// f_123 revoked" is the whole point of keeping them apart.
type protocolError struct {
	// cause is what is recorded. Never sent.
	cause error

	code        string
	description string
	status      int
}

// Error implements error, rendering what would be recorded rather than what
// would be sent.
func (e *protocolError) Error() string {
	if e.cause != nil {
		return e.code + ": " + e.cause.Error()
	}

	return e.code + ": " + e.description
}

// Unwrap exposes the cause, so a caller — a test, most usefully — can match the
// sentinel behind a rendered response.
func (e *protocolError) Unwrap() error { return e.cause }

// newProtocolError builds an error response.
func newProtocolError(status int, code, description string, cause error) *protocolError {
	return &protocolError{status: status, code: code, description: description, cause: cause}
}

// errorBody is the JSON an OAuth error response carries.
type errorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// writeJSON writes a JSON body with the no-store headers every OAuth endpoint
// response carries.
//
// RFC 6749 §5.1 requires them on the token endpoint, and they are right on the
// others for the same reason: every one of these responses either contains a
// credential or describes one.
func writeJSON(res http.ResponseWriter, status int, body any) {
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("Cache-Control", "no-store")
	res.Header().Set("Pragma", "no-cache")
	res.WriteHeader(status)

	writeBody(res, body)
}

// writeBody encodes a response body, after the status is already on the wire.
//
// A failed encode has no error page left to send, so the client sees a
// truncated body — which is what a dropped connection looks like anyway. It is
// one function rather than an inlined encode at each site so that the two
// endpoints writing JSON without an OAuth status (the metadata documents) go
// through the same path as the ones with one.
func writeBody(res http.ResponseWriter, body any) {
	//nolint:errcheck,errchkjson // the status is already on the wire; see the doc comment.
	_ = json.NewEncoder(res).Encode(body)
}

// writeProtocolError renders an error response as JSON.
//
// A 401 carrying invalid_client also carries the WWW-Authenticate challenge RFC
// 6749 §5.2 requires, without which a client using HTTP Basic has no way to
// know it should have.
func writeProtocolError(res http.ResponseWriter, err *protocolError) {
	if err.status == http.StatusUnauthorized {
		res.Header().Set("WWW-Authenticate", `Basic realm="oauth2", charset="UTF-8"`)
	}

	writeJSON(res, err.status, errorBody{Error: err.code, ErrorDescription: err.description})
}
