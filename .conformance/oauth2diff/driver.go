// Package oauth2diff drives identical OAuth 2.1 flows against two
// authorization servers — this module's authentication/oauth2server and
// github.com/go-oauth2/oauth2/v4 — and reports where they disagree.
//
// # Why this exists
//
// authentication/oauth2server is a from-scratch implementation of a protocol
// nobody on this project can audit by reading. go-oauth2/oauth2 is widely
// deployed and has been for a decade. Neither fact is evidence on its own, and
// "read it carefully" does not scale to a spec this size.
//
// What is checkable is agreement. Where the two servers implement the same
// thing, they should answer the same way, and every disagreement is either a
// defect here or a deliberate divergence that somebody has to write down. This
// package turns that into a test: cases marked mustAgree fail the build when
// the answers differ, and cases marked divergence fail the build when they
// stop differing — because a documented divergence that silently disappeared
// means the reasoning behind it is now wrong.
//
// # What it is not
//
// It is not a conformance suite. Agreement with go-oauth2 is evidence about
// the overlap, and the overlap is roughly /authorize and /token. Dynamic
// client registration, discovery, revocation and protected resource metadata
// have no counterpart in the library and are not covered here — see
// spec_test.go for those, which check the RFCs' MUSTs directly.
package oauth2diff

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// target is one authorization server under test, already running, with one
// client registered on it.
type target struct {
	// authorize performs the /authorize leg including whatever this server
	// requires to identify the resource owner. The two servers differ here by
	// construction — one renders a login form and takes a POST, the other
	// takes a handler-supplied user ID on the GET — and that difference is not
	// what is being compared.
	authorize func(t *testing.T, query url.Values) *http.Response

	name         string
	baseURL      string
	clientID     string
	clientSecret string
	redirectURI  string
}

// outcome is the comparable part of a server's answer.
//
// Deliberately not the whole response: token values, expiry stamps and
// error_description strings differ between any two implementations without
// either being wrong. What the RFCs actually pin down is the status code and
// the error code, so that is what gets compared.
type outcome struct {
	// Error is the RFC 6749 §5.2 error code, empty on success.
	Error string

	// Redirect is the error code delivered via the redirect URI's query
	// string rather than a response body, which is how /authorize reports a
	// failure once the redirect URI is known to be the client's.
	Redirect string

	Status int

	// Browser records a failure answered as a page rather than as a protocol
	// error. It is the correct answer at /authorize whenever the redirect URI
	// has not been established as the client's, because there is nowhere safe
	// to send the error to — so it is an outcome kind, not a parse failure.
	Browser bool

	CodeIssued      bool
	HasAccessToken  bool
	HasRefreshToken bool
}

// String renders an outcome for the comparison table.
func (o outcome) String() string {
	switch {
	case o.Error != "":
		return itoa(o.Status) + " " + o.Error
	case o.Redirect != "":
		return itoa(o.Status) + " redirect:" + o.Redirect
	case o.Browser:
		return itoa(o.Status) + " answered-in-browser"
	case o.HasAccessToken && o.HasRefreshToken:
		return itoa(o.Status) + " access+refresh"
	case o.HasAccessToken:
		return itoa(o.Status) + " access"
	case o.CodeIssued:
		return itoa(o.Status) + " code-issued"
	default:
		return itoa(o.Status)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "-"
	}

	const digits = "0123456789"

	var b []byte
	for i > 0 {
		b = append([]byte{digits[i%10]}, b...)
		i /= 10
	}

	return string(b)
}

// pkcePair returns a verifier and its S256 challenge.
func pkcePair(t *testing.T) (verifier, challenge string) {
	t.Helper()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generating PKCE verifier: %v", err)
	}

	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])

	return verifier, challenge
}

// noRedirect is an http.Client that reports the 302 rather than following it.
// The Location header is the entire result of a successful /authorize.
func noRedirect() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// authorizeQuery builds a well-formed /authorize query, which each case then
// damages in exactly one way.
func (tg *target) authorizeQuery(challenge string) url.Values {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {tg.clientID},
		"redirect_uri":          {tg.redirectURI},
		"scope":                 {"read"},
		"state":                 {"the-state"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
}

// code runs the /authorize leg and extracts the authorization code from the
// redirect. It returns the code and the outcome; on failure the code is empty
// and the outcome says how it failed.
func (tg *target) code(t *testing.T, query url.Values) (string, outcome) {
	t.Helper()

	res := tg.authorize(t, query)
	defer func() { _ = res.Body.Close() }()

	out := outcome{Status: res.StatusCode}

	location := res.Header.Get("Location")
	if location == "" {
		// No redirect: the failure was answered in the browser, because the
		// redirect URI was not established as the client's.
		if code := errorCodeFromBody(t, res); code != "" {
			out.Error = code
		} else {
			out.Browser = true
		}

		return "", out
	}

	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("%s: unparseable Location %q: %v", tg.name, location, err)
	}

	q := parsed.Query()
	if e := q.Get("error"); e != "" {
		out.Redirect = e

		return "", out
	}

	issued := q.Get("code")
	out.CodeIssued = issued != ""

	return issued, out
}

// redeem exchanges an authorization code at /token.
func (tg *target) redeem(t *testing.T, form url.Values) outcome {
	t.Helper()

	return tg.postToken(t, form)
}

// postToken posts to /token with client_secret_post credentials, which both
// servers accept, and reads back the comparable part of the answer.
func (tg *target) postToken(t *testing.T, form url.Values) outcome {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		tg.baseURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("%s: building token request: %v", tg.name, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := noRedirect().Do(req)
	if err != nil {
		t.Fatalf("%s: posting to /token: %v", tg.name, err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("%s: reading /token response: %v", tg.name, err)
	}

	var payload struct {
		Error        string `json:"error"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}

	// A non-JSON body is itself a disagreement worth surfacing rather than
	// failing on, so an unparseable body leaves the zero payload in place.
	_ = json.Unmarshal(body, &payload)

	return outcome{
		Status:          res.StatusCode,
		Error:           payload.Error,
		HasAccessToken:  payload.AccessToken != "",
		HasRefreshToken: payload.RefreshToken != "",
	}
}

// errorCodeFromBody reads an RFC 6749 error code out of a response body,
// tolerating a server that answered with HTML instead.
func errorCodeFromBody(t *testing.T, res *http.Response) string {
	t.Helper()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return ""
	}

	var payload struct {
		Error string `json:"error"`
	}

	if err = json.Unmarshal(body, &payload); err != nil {
		// An HTML page, which is what a browser-answered failure looks like.
		return ""
	}

	return payload.Error
}

// tokenForm builds a well-formed authorization_code token request.
func (tg *target) tokenForm(code, verifier string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {tg.redirectURI},
		"client_id":     {tg.clientID},
		"client_secret": {tg.clientSecret},
		"code_verifier": {verifier},
	}
}
