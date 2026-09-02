package oauth2server_test

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/primandproper/platform-go/v14/authentication/oauth2server"
	loggingnoop "github.com/primandproper/platform-go/v14/observability/logging/noop"
	tracingnoop "github.com/primandproper/platform-go/v14/observability/tracing/noop"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// otherResource is a second protected resource behind the same authorization
// server, which is the shape the audience check exists for.
const otherResource = "https://mcp.example/"

// newVerifier builds a Verifier over a harness's server for the resource the
// harness's tokens are minted for.
func newVerifier(t *testing.T, h *harness, resource string) *oauth2server.Verifier {
	t.Helper()

	meta, err := oauth2server.NewResourceMetadata(resource, []string{testIssuer},
		oauth2server.WithResourceScopes("read", "write"))
	must.NoError(t, err)

	verifier, err := oauth2server.NewVerifier(meta, h.server,
		oauth2server.WithVerifierLogger(loggingnoop.NewLogger()),
		oauth2server.WithVerifierTracerProvider(tracingnoop.NewTracerProvider()))
	must.NoError(t, err)

	return verifier
}

// issuedToken runs the whole flow and hands back a live access token minted for
// testResource with the "read" scope.
func issuedToken(t *testing.T, h *harness) string {
	t.Helper()

	return h.exchange(h.registerConfidential()).AccessToken
}

func TestNewVerifier(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		test.EqOp(t, testResource, verifier.Metadata().Document().Resource)
	})

	T.Run("refuses a verifier with nothing to compare against", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)

		verifier, err := oauth2server.NewVerifier(nil, h.server)
		test.ErrorIs(t, err, oauth2server.ErrNilResourceMetadata)
		test.Nil(t, verifier)
	})

	T.Run("refuses a verifier with nothing to verify against", func(t *testing.T) {
		t.Parallel()

		meta, err := oauth2server.NewResourceMetadata(testResource, []string{testIssuer})
		must.NoError(t, err)

		verifier, err := oauth2server.NewVerifier(meta, nil)
		test.ErrorIs(t, err, oauth2server.ErrNilTokenAuthenticator)
		test.Nil(t, verifier)
	})
}

func TestVerifier_Verify(T *testing.T) {
	T.Parallel()

	T.Run("admits a live token minted for this resource", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		token, err := verifier.Verify(t.Context(), issuedToken(t, h))
		must.NoError(t, err)

		test.EqOp(t, testSubject().ID, token.Subject.ID)
		test.Eq(t, []string{"read"}, token.Scopes)
		test.Eq(t, []string{testResource}, token.Audience)
	})

	T.Run("refuses a request carrying nothing", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		_, err := verifier.Verify(t.Context(), "  ")
		test.ErrorIs(t, err, oauth2server.ErrNoBearerToken)
	})

	T.Run("refuses a token nobody issued", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		_, err := verifier.Verify(t.Context(), "not-a-token")
		test.ErrorIs(t, err, oauth2server.ErrNotFound)
	})

	T.Run("refuses a token the moment it is revoked", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		reg := h.registerConfidential()
		pair := h.exchange(reg)

		// Good here first, so that the refusal below is the revocation and not
		// some other reason this token was never going to work.
		_, err := verifier.Verify(t.Context(), pair.AccessToken)
		must.NoError(t, err)

		test.EqOp(t, http.StatusOK, h.revoke(reg.ClientID, reg.ClientSecret, pair.AccessToken, ""))

		// The whole argument for opaque tokens: this is a refusal now rather
		// than at the end of the token's lifetime.
		_, err = verifier.Verify(t.Context(), pair.AccessToken)
		test.ErrorIs(t, err, oauth2server.ErrNotFound)
	})

	// The case this type exists for: a token that is live, and is not this
	// resource server's to accept.
	T.Run("refuses a live token minted for another resource", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)

		// Same authorization server, same store, a different protected
		// resource. Without the audience comparison this token works here.
		verifier := newVerifier(t, h, otherResource)

		_, err := verifier.Verify(t.Context(), issuedToken(t, h))
		test.ErrorIs(t, err, oauth2server.ErrTokenAudienceMismatch)
	})

	T.Run("refuses a token carrying no audience at all", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		// An authorization request with no resource indicator mints a token
		// with an empty audience, which is exactly the token that can be
		// replayed anywhere.
		reg := h.registerConfidential()
		params := authorizeParams(reg.ClientID)
		params.Del("resource")

		pair := h.redeem(reg, h.codeFrom(h.authorize(params, login())))
		must.EqOp(t, http.StatusOK, pair.status)

		_, err := verifier.Verify(t.Context(), pair.AccessToken)
		test.ErrorIs(t, err, oauth2server.ErrTokenAudienceMismatch)
	})

	T.Run("refuses a token that lacks a required scope", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		_, err := verifier.Verify(t.Context(), issuedToken(t, h), "read", "write")
		test.ErrorIs(t, err, oauth2server.ErrInsufficientScope)
	})

	T.Run("admits a token holding every required scope", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		verifier := newVerifier(t, h, testResource)

		token, err := verifier.Verify(t.Context(), issuedToken(t, h), "read")
		must.NoError(t, err)
		test.NotNil(t, token)
	})

	// A broken store is the one failure that is this server's fault, and the
	// one that must not read as "your credential is bad".
	T.Run("reports a store failure as itself", func(t *testing.T) {
		t.Parallel()

		store := newFaultStore()
		h := newStoreHarness(t, store)
		verifier := newVerifier(t, h, testResource)

		store.breaks(methodGetAccessToken, errStoreDown)

		_, err := verifier.Verify(t.Context(), "anything")
		must.Error(t, err)
		test.ErrorIs(t, err, errStoreDown)

		// Not one of the refusals a client caused. A resource server that
		// collapsed this into "invalid_token" would send every client through
		// discovery and re-registration while its own database was down.
		test.False(t, isRefusal(err))
	})
}

// isRefusal reports whether an error is one of the refusals a client caused,
// as opposed to a fault this server owns.
func isRefusal(err error) bool {
	for _, sentinel := range []error{
		oauth2server.ErrNoBearerToken,
		oauth2server.ErrNotFound,
		oauth2server.ErrTokenAudienceMismatch,
		oauth2server.ErrInsufficientScope,
	} {
		if stderrors.Is(err, sentinel) {
			return true
		}
	}

	return false
}

func TestVerifier_Middleware(T *testing.T) {
	T.Parallel()

	// guarded stands up a resource server: one handler behind one Verifier,
	// which is the whole of what mounting an MCP server behind this package's
	// tokens takes.
	guarded := func(t *testing.T, h *harness, resource string, scopes ...string) *httptest.Server {
		t.Helper()

		verifier := newVerifier(t, h, resource)

		handler := verifier.Middleware(scopes...)(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			token, ok := oauth2server.TokenFromContext(req.Context())
			must.True(t, ok)

			res.WriteHeader(http.StatusOK)
			_, _ = res.Write([]byte(token.Subject.ID))
		}))

		resourceServer := httptest.NewServer(handler)
		t.Cleanup(resourceServer.Close)

		return resourceServer
	}

	call := func(t *testing.T, srv *httptest.Server, authorization string) *http.Response {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
		must.NoError(t, err)

		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}

		res, err := srv.Client().Do(req)
		must.NoError(t, err)
		t.Cleanup(func() { _ = res.Body.Close() })

		return res
	}

	T.Run("admits a good token and hands it to the handler", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		srv := guarded(t, h, testResource, "read")

		res := call(t, srv, "Bearer "+issuedToken(t, h))

		test.EqOp(t, http.StatusOK, res.StatusCode)
		test.EqOp(t, testSubject().ID, readBody(t, res))
	})

	T.Run("matches the scheme case-insensitively", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		srv := guarded(t, h, testResource)

		// RFC 9110 §11.1: the scheme is case-insensitive, and clients spell it
		// every way.
		test.EqOp(t, http.StatusOK, call(t, srv, "bearer "+issuedToken(t, h)).StatusCode)
	})

	// The half of discovery that is easy to leave out: a client that was never
	// configured with this server has to be told where to look.
	T.Run("challenges a request carrying no token, without naming an error", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		res := call(t, guarded(t, h, testResource), "")

		test.EqOp(t, http.StatusUnauthorized, res.StatusCode)

		challenge := res.Header.Get("WWW-Authenticate")
		test.StrContains(t, challenge,
			`resource_metadata="https://api.example/.well-known/oauth-protected-resource"`)

		// No error code: the client has not got anything wrong yet, it has not
		// tried.
		test.StrNotContains(t, challenge, "error=")
		test.EqOp(t, "no-store", res.Header.Get("Cache-Control"))
	})

	T.Run("challenges a header that is not a bearer credential", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		srv := guarded(t, h, testResource)

		for _, header := range []string{
			"Basic Zm9vOmJhcg==",
			"Bearer",
			issuedToken(t, h),
		} {
			res := call(t, srv, header)
			test.EqOp(t, http.StatusUnauthorized, res.StatusCode)
			test.StrNotContains(t, res.Header.Get("WWW-Authenticate"), "error=")
		}
	})

	T.Run("challenges an unusable token as invalid_token", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		res := call(t, guarded(t, h, testResource), "Bearer nonsense")

		test.EqOp(t, http.StatusUnauthorized, res.StatusCode)
		test.StrContains(t, res.Header.Get("WWW-Authenticate"), `error="invalid_token"`)
	})

	T.Run("challenges a token minted for another resource as invalid_token", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		res := call(t, guarded(t, h, otherResource), "Bearer "+issuedToken(t, h))

		test.EqOp(t, http.StatusUnauthorized, res.StatusCode)
		test.StrContains(t, res.Header.Get("WWW-Authenticate"), `error="invalid_token"`)
	})

	// The one refusal that is a 403: the credential is good, and re-presenting
	// it will not help — so the challenge says what to go and ask for instead.
	T.Run("refuses a missing scope with 403 and names the scope", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		res := call(t, guarded(t, h, testResource, "read", "write"), "Bearer "+issuedToken(t, h))

		test.EqOp(t, http.StatusForbidden, res.StatusCode)

		challenge := res.Header.Get("WWW-Authenticate")
		test.StrContains(t, challenge, `error="insufficient_scope"`)
		test.StrContains(t, challenge, `scope="read write"`)
	})

	T.Run("answers a broken store with a 500 and no challenge", func(t *testing.T) {
		t.Parallel()

		store := newFaultStore()
		h := newStoreHarness(t, store)
		srv := guarded(t, h, testResource)

		store.breaks(methodGetAccessToken, errStoreDown)

		res := call(t, srv, "Bearer anything")

		test.EqOp(t, http.StatusInternalServerError, res.StatusCode)

		// No challenge. The client's credential is not what went wrong, and
		// telling it to re-register would aim a retry storm at the
		// authorization server.
		test.EqOp(t, "", res.Header.Get("WWW-Authenticate"))
	})

	T.Run("does not send the body downstream when it refuses", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)

		var reached bool
		verifier := newVerifier(t, h, testResource)
		handler := verifier.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			reached = true
		}))

		res := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp", strings.NewReader(`{}`))
		handler.ServeHTTP(res, req)

		test.False(t, reached)
		test.EqOp(t, http.StatusUnauthorized, res.Code)
	})
}

func TestVerifier_WriteChallenge(T *testing.T) {
	T.Parallel()

	// ErrInsufficientScope is exported, so a resource server verifying for
	// itself can hand back a bare one. It carries no scope list, and that has
	// to render a challenge naming none rather than fail to render one.
	T.Run("renders a bare insufficient-scope sentinel", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		res := httptest.NewRecorder()

		newVerifier(t, h, testResource).WriteChallenge(res, oauth2server.ErrInsufficientScope)

		test.EqOp(t, http.StatusForbidden, res.Code)

		challenge := res.Header().Get("WWW-Authenticate")
		test.StrContains(t, challenge, `error="insufficient_scope"`)
		test.StrNotContains(t, challenge, "scope=\"\"")
	})

	// Anything this package did not produce is a fault rather than a refusal,
	// and a fault must not tell a client its credential is bad.
	T.Run("answers an unrecognized error with a 500 and no challenge", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		res := httptest.NewRecorder()

		newVerifier(t, h, testResource).WriteChallenge(res, errStoreDown)

		test.EqOp(t, http.StatusInternalServerError, res.Code)
		test.EqOp(t, "", res.Header().Get("WWW-Authenticate"))
	})
}

func TestBearerFromRequest(T *testing.T) {
	T.Parallel()

	T.Run("reads the credential out of an Authorization header", func(t *testing.T) {
		t.Parallel()

		for header, expected := range map[string]string{
			"Bearer abc123":   "abc123",
			"bearer abc123":   "abc123",
			"BEARER abc123":   "abc123",
			"Bearer  abc123 ": "abc123",
			"Basic abc123":    "",
			"Bearer":          "",
			"abc123":          "",
			"":                "",
		} {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody)
			if header != "" {
				req.Header.Set("Authorization", header)
			}

			test.EqOp(t, expected, oauth2server.BearerFromRequest(req))
		}
	})

	// A query parameter is what RFC 6750 §2.3 defines and what
	// NewResourceMetadata declines to advertise; an extractor that read it
	// anyway would be advertising it after all.
	T.Run("does not read a token out of the query string", func(t *testing.T) {
		t.Parallel()

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?access_token=abc123", http.NoBody)
		test.EqOp(t, "", oauth2server.BearerFromRequest(req))
	})

	T.Run("tolerates no request at all", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, "", oauth2server.BearerFromRequest(nil))
	})
}

func TestTokenFromContext(T *testing.T) {
	T.Parallel()

	T.Run("round-trips a verified token", func(t *testing.T) {
		t.Parallel()

		want := &oauth2server.AccessToken{Subject: oauth2server.Subject{ID: "user_1"}}

		got, ok := oauth2server.TokenFromContext(oauth2server.ContextWithToken(t.Context(), want))
		must.True(t, ok)
		test.EqOp(t, want, got)
	})

	// A handler mounted somewhere unguarded sees this, which is why the second
	// return exists rather than a bare dereference.
	T.Run("reports absence rather than handing back a nil token", func(t *testing.T) {
		t.Parallel()

		token, ok := oauth2server.TokenFromContext(context.Background())
		test.False(t, ok)
		test.Nil(t, token)
	})
}

func TestAccessToken_HasScopes(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		token := &oauth2server.AccessToken{Scopes: []string{"recipes:read", "recipes:write"}}

		test.True(t, token.HasScopes())
		test.True(t, token.HasScopes("recipes:read"))
		test.True(t, token.HasScopes("recipes:write", "recipes:read"))
		test.False(t, token.HasScopes("recipes:read", "meals:read"))
	})

	// No hierarchy, in either direction. RFC 6749 §3.3 leaves scope semantics
	// to the authorization server, so an implication invented here would be one
	// this package granted on a deployment's behalf.
	T.Run("reads no hierarchy into a scope string", func(t *testing.T) {
		t.Parallel()

		token := &oauth2server.AccessToken{Scopes: []string{"recipes:write"}}

		test.False(t, token.HasScopes("recipes:read"))
		test.False(t, token.HasScopes("recipes"))
	})

	T.Run("a nil token carries nothing", func(t *testing.T) {
		t.Parallel()

		var token *oauth2server.AccessToken

		test.True(t, token.HasScopes())
		test.False(t, token.HasScopes("recipes:read"))
	})
}

func TestResourceMetadata_ScopeChallenge(T *testing.T) {
	T.Parallel()

	T.Run("names the scopes that would have satisfied the request", func(t *testing.T) {
		t.Parallel()

		meta, err := oauth2server.NewResourceMetadata(testResource, []string{testIssuer})
		must.NoError(t, err)

		test.EqOp(t,
			`Bearer resource_metadata="https://api.example/.well-known/oauth-protected-resource", `+
				`error="insufficient_scope", error_description="needs more", scope="read write"`,
			meta.ScopeChallenge("needs more", []string{"read", "write"}))
	})

	T.Run("omits the attribute when there is nothing to name", func(t *testing.T) {
		t.Parallel()

		meta, err := oauth2server.NewResourceMetadata(testResource, []string{testIssuer})
		must.NoError(t, err)

		test.StrNotContains(t, meta.ScopeChallenge("", nil), "scope=")
	})
}
