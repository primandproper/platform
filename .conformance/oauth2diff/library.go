package oauth2diff

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
)

// libraryClientID and libraryClientSecret are fixed rather than issued: the
// library has no registration endpoint, which is the first thing the
// comparison establishes.
const (
	libraryClientID     = "diff-harness-client"
	libraryClientSecret = "diff-harness-secret"
)

// startLibrary brings up go-oauth2/oauth2/v4 configured as close to OAuth 2.1
// as its options allow.
//
// The configuration here is the honest version of "just use the library": every
// line below is a decision someone has to know to make, and the defaults it
// overrides are the defaults a consumer who did not know would ship.
func startLibrary(t *testing.T, redirectURI string) *target {
	t.Helper()

	manager := manage.NewDefaultManager()
	manager.MustTokenStorage(store.NewMemoryTokenStore())

	// Rotation on every refresh, with the old pair invalidated. Not the
	// default: NewDefaultManager leaves the refresh token in place, so a
	// refresh token stays valid for its whole lifetime.
	manager.SetRefreshTokenCfg(&manage.RefreshingConfig{
		IsGenerateRefresh:  true,
		IsRemoveAccess:     true,
		IsRemoveRefreshing: true,
	})

	manager.SetAuthorizeCodeTokenCfg(&manage.Config{
		AccessTokenExp:    15 * time.Minute,
		RefreshTokenExp:   7 * 24 * time.Hour,
		IsGenerateRefresh: true,
	})

	clients := store.NewClientStore()
	if err := clients.Set(libraryClientID, &models.Client{
		ID:     libraryClientID,
		Secret: libraryClientSecret,
		Domain: redirectURI,
		UserID: subjectUsername,
	}); err != nil {
		t.Fatalf("seeding library client store: %v", err)
	}

	manager.MapClientStorage(clients)

	// DefaultValidateURI is left in place deliberately. It is what a consumer
	// gets without knowing to replace it, and TestRedirectURIMatching measures
	// exactly what that costs.

	srv := server.NewServer(server.NewConfig(), manager)

	// OAuth 2.1: no implicit, no ROPC, no client_credentials for this client,
	// no PKCE downgrade to plain, and PKCE required rather than optional. All
	// five are off the shipped defaults in server.NewConfig.
	srv.SetAllowedResponseType(oauth2.Code)
	srv.SetAllowedGrantType(oauth2.AuthorizationCode, oauth2.Refreshing)
	srv.Config.AllowedCodeChallengeMethods = []oauth2.CodeChallengeMethod{oauth2.CodeChallengeS256}
	srv.Config.ForcePKCE = true

	// Read client credentials from the form rather than only from Basic auth,
	// which is what ClientBasicHandler — the default — restricts them to.
	srv.SetClientInfoHandler(server.ClientFormHandler)

	srv.SetUserAuthorizationHandler(func(_ http.ResponseWriter, req *http.Request) (string, error) {
		if req.FormValue("username") != subjectUsername {
			return "", errors.New("no such user")
		}

		return subjectUsername, nil
	})

	mux := http.NewServeMux()

	mux.HandleFunc("/authorize", func(res http.ResponseWriter, req *http.Request) {
		if err := srv.HandleAuthorizeRequest(res, req); err != nil {
			http.Error(res, err.Error(), http.StatusBadRequest)
		}
	})

	mux.HandleFunc("/token", func(res http.ResponseWriter, req *http.Request) {
		if err := srv.HandleTokenRequest(res, req); err != nil {
			http.Error(res, err.Error(), http.StatusBadRequest)
		}
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	tg := &target{
		name:         "go-oauth2",
		baseURL:      ts.URL,
		clientID:     libraryClientID,
		clientSecret: libraryClientSecret,
		redirectURI:  redirectURI,
	}

	// The library has no login form: the user authorization handler reads the
	// credential off whatever request arrives, so the whole leg is one GET.
	tg.authorize = func(t *testing.T, query url.Values) *http.Response {
		t.Helper()

		q := url.Values{}
		for k, v := range query {
			q[k] = v
		}

		q.Set("username", subjectUsername)

		req, reqErr := http.NewRequestWithContext(t.Context(), http.MethodGet,
			ts.URL+"/authorize?"+q.Encode(), nil)
		if reqErr != nil {
			t.Fatalf("go-oauth2: building authorize request: %v", reqErr)
		}

		res, doErr := noRedirect().Do(req)
		if doErr != nil {
			t.Fatalf("go-oauth2: getting /authorize: %v", doErr)
		}

		return res
	}

	return tg
}
