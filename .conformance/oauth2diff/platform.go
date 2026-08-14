package oauth2diff

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v10/authentication/oauth2server"
	"github.com/primandproper/platform-go/v10/authentication/oauth2server/memory"
)

// subjectUsername is the only credential either server accepts, so that the
// authentication seam contributes nothing to the comparison.
const subjectUsername = "alice"

// startPlatform brings up this module's authorization server and registers one
// confidential client on it through /register, which is the flow a real client
// takes and exercises dynamic registration on the way past.
func startPlatform(t *testing.T, redirectURI string) *target {
	t.Helper()

	mux := http.NewServeMux()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	authenticator := oauth2server.SubjectAuthenticatorFunc(
		func(_ context.Context, req *http.Request) (*oauth2server.Subject, error) {
			if req.FormValue("username") != subjectUsername {
				return nil, oauth2server.NewLoginError("no", nil)
			}

			return &oauth2server.Subject{ID: subjectUsername}, nil
		})

	// The issuer is the test server's own URL: loopback http, which
	// normalizeIssuer permits and which every metadata document and audience
	// check is then derived from.
	srv, err := oauth2server.NewServer(ts.URL, memory.NewStore(), authenticator,
		oauth2server.WithScopes("read"))
	if err != nil {
		t.Fatalf("building platform server: %v", err)
	}

	mux.Handle("/", srv.Handler())

	tg := &target{
		name:        "platform",
		baseURL:     ts.URL,
		redirectURI: redirectURI,
	}

	tg.clientID, tg.clientSecret = registerPlatformClient(t, ts.URL, redirectURI)

	// The platform server renders a login form on GET and issues the code on
	// the POST that carries the credential, with the authorization parameters
	// staying in the query string across both.
	tg.authorize = func(t *testing.T, query url.Values) *http.Response {
		t.Helper()

		form := url.Values{"username": {subjectUsername}}

		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodPost,
			ts.URL+oauth2server.PathAuthorize+"?"+query.Encode(),
			strings.NewReader(form.Encode()))
		if reqErr != nil {
			t.Fatalf("platform: building authorize request: %v", reqErr)
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		res, doErr := noRedirect().Do(req)
		if doErr != nil {
			t.Fatalf("platform: posting to /authorize: %v", doErr)
		}

		return res
	}

	return tg
}

// registerPlatformClient performs RFC 7591 dynamic registration and returns the
// issued credentials. The secret is readable exactly here — the store holds a
// digest — which is itself one of the differences this harness exists to make
// visible.
func registerPlatformClient(t *testing.T, baseURL, redirectURI string) (clientID, clientSecret string) {
	t.Helper()

	body, err := json.Marshal(oauth2server.RegistrationRequest{
		ClientName:              "diff-harness",
		TokenEndpointAuthMethod: oauth2server.AuthMethodClientSecret,
		Scope:                   "read",
		RedirectURIs:            []string{redirectURI},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
	})
	if err != nil {
		t.Fatalf("marshaling registration: %v", err)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		baseURL+oauth2server.PathRegister, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("building registration request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := noRedirect().Do(req)
	if err != nil {
		t.Fatalf("posting registration: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusCreated {
		t.Fatalf("registration answered %d, want 201", res.StatusCode)
	}

	var registered oauth2server.RegistrationResponse
	if err = json.NewDecoder(res.Body).Decode(&registered); err != nil {
		t.Fatalf("decoding registration response: %v", err)
	}

	if registered.ClientSecret == "" {
		t.Fatal("registration issued no client secret for a confidential client")
	}

	return registered.ClientID, registered.ClientSecret
}
