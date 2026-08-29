package oauth2server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/primandproper/platform-go/v13/authentication/oauth2server"
	"github.com/primandproper/platform-go/v13/authentication/oauth2server/memory"
	"github.com/primandproper/platform-go/v13/ratelimiting"
	ratelimitinghttp "github.com/primandproper/platform-go/v13/ratelimiting/http"
)

// The two seams a deployment supplies: who the human is, and what a token
// means. Everything else in this package is protocol.
func ExampleNewServer() {
	// Whatever this application actually does to identify a human — a password
	// and a TOTP code against its own identity repository, an existing session
	// cookie, a corporate identity provider. There is no default, because a
	// default would be a server that issues authorization codes to whoever
	// asks.
	authenticator := oauth2server.SubjectAuthenticatorFunc(
		func(_ context.Context, req *http.Request) (*oauth2server.Subject, error) {
			username := req.PostFormValue(oauth2server.FieldUsername)
			if username == "" {
				return nil, oauth2server.NewLoginError("Enter your username.", nil)
			}

			return &oauth2server.Subject{
				ID: "user_" + username,
				// The application-shaped half of the identity. This package
				// carries it into every token and never reads it.
				Claims: map[string]string{"account_id": "acct_9"},
			}, nil
		})

	// memory for this example; a deployment wants oauth2server/database, or two
	// replicas cannot complete each other's logins.
	srv, err := oauth2server.NewServer("https://auth.example", memory.NewStore(), authenticator,
		oauth2server.WithScopes("recipes:read", "recipes:write"),
		oauth2server.WithResources("https://api.example/"))
	if err != nil {
		panic(err)
	}

	// srv.Mount(router) registers all six endpoints; srv.Handler() is the same
	// set as one http.Handler.
	doc := srv.Metadata()

	fmt.Println(doc.TokenEndpoint)
	fmt.Println(doc.CodeChallengeMethodsSupported)
	fmt.Println(doc.GrantTypesSupported)

	// Output:
	// https://auth.example/token
	// [S256]
	// [authorization_code refresh_token]
}

// /register is unauthenticated by construction, so a deployment bounds it with
// a gate of its own choosing rather than one this package guessed at.
func ExampleWithRegistrationLimiter() {
	// One registration per second, and no burst beyond it. A real deployment
	// picks these from its own traffic; what it cannot delegate is the next
	// line.
	limiter, err := ratelimiting.NewInMemoryRateLimiter(1, 1)
	if err != nil {
		panic(err)
	}
	defer func() { _ = limiter.Close() }()

	// The address the connection came from, which is right for a server facing
	// clients directly and wrong behind a proxy — there, KeyByForwardedFor with
	// the number of proxies actually in front. That is the fact this package
	// cannot know and the deployment cannot avoid knowing.
	gate, err := ratelimitinghttp.NewMiddleware(limiter, ratelimitinghttp.KeyByRemoteAddr())
	if err != nil {
		panic(err)
	}

	authenticator := oauth2server.SubjectAuthenticatorFunc(
		func(_ context.Context, _ *http.Request) (*oauth2server.Subject, error) {
			return &oauth2server.Subject{ID: "user_1"}, nil
		})

	srv, err := oauth2server.NewServer("https://auth.example", memory.NewStore(), authenticator,
		oauth2server.WithRegistrationLimiter(gate))
	if err != nil {
		panic(err)
	}

	// The gate is inside RegisterHandler, so Handler, Mount, and a deployment
	// routing POST /register by hand are all behind it.
	front := httptest.NewServer(srv.Handler())
	defer front.Close()

	register := func() int {
		body := strings.NewReader(`{"redirect_uris":["https://client.example/callback"]}`)

		req, reqErr := http.NewRequestWithContext(context.Background(),
			http.MethodPost, front.URL+oauth2server.PathRegister, body)
		if reqErr != nil {
			panic(reqErr)
		}
		req.Header.Set("Content-Type", "application/json")

		res, doErr := front.Client().Do(req)
		if doErr != nil {
			panic(doErr)
		}
		defer func() { _ = res.Body.Close() }()

		return res.StatusCode
	}

	fmt.Println(register())
	fmt.Println(register())

	// Output:
	// 201
	// 429
}

// A resource owner who is already authenticated by other means never sees a
// form, on either verb.
func ExampleWithSubjectResolver() {
	// The seam for clients that hold proof rather than a keyboard: a
	// first-party application with a session cookie, a CLI with a token, a
	// service exchanging one credential for another. Returning (nil, nil) means
	// "not one of mine", and the login form is rendered as usual.
	resolver := oauth2server.SubjectResolverFunc(
		func(_ context.Context, req *http.Request) (*oauth2server.Subject, error) {
			// A request with no session cookie is not this resolver's, which
			// is (nil, nil) rather than an error: the form is still the right
			// answer for whoever sent it.
			session, _ := req.Cookie("session")
			if session == nil {
				return nil, nil
			}

			return &oauth2server.Subject{ID: "user_" + session.Value}, nil
		})

	store := memory.NewStore()

	srv, err := oauth2server.NewServer("https://auth.example", store,
		oauth2server.SubjectAuthenticatorFunc(
			func(context.Context, *http.Request) (*oauth2server.Subject, error) {
				return nil, oauth2server.NewLoginError("Sign in to continue.", nil)
			}),
		oauth2server.WithSubjectResolver(resolver))
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	if err = store.CreateClient(ctx, &oauth2server.Client{
		ID:           "client_1",
		RedirectURIs: []string{"https://app.example/callback"},
	}); err != nil {
		panic(err)
	}

	query := url.Values{
		"response_type":         {oauth2server.ResponseTypeCode},
		"client_id":             {"client_1"},
		"redirect_uri":          {"https://app.example/callback"},
		"code_challenge":        {oauth2server.S256Challenge("0123456789012345678901234567890123456789abc")},
		"code_challenge_method": {oauth2server.CodeChallengeMethodS256},
	}

	// A GET, with no body to POST and nothing to type.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, oauth2server.PathAuthorize+"?"+query.Encode(), http.NoBody)
	req.AddCookie(&http.Cookie{Name: "session", Value: "1"})

	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	location, err := url.Parse(res.Header().Get("Location"))
	if err != nil {
		panic(err)
	}

	fmt.Println(res.Code)
	fmt.Println(location.Query().Has("code"))

	// Output:
	// 302
	// true
}

// A resource server publishes its own document, so a client that discovered it
// at runtime can find the authorization server behind it and register.
func ExampleNewResourceMetadata() {
	meta, err := oauth2server.NewResourceMetadata(
		"https://api.example/",
		[]string{"https://auth.example"},
		oauth2server.WithResourceName("Recipes API"),
		oauth2server.WithResourceScopes("recipes:read"))
	if err != nil {
		panic(err)
	}

	// Sent with every 401, so a client that was never configured with this
	// server is told where to look rather than simply refused.
	fmt.Println(meta.Challenge("invalid_token", "the token expired"))

	// Output:
	// Bearer resource_metadata="https://api.example/.well-known/oauth-protected-resource", error="invalid_token", error_description="the token expired"
}

// The resource server's half: an MCP endpoint mounted behind this package's
// tokens.
//
// Nothing here is MCP-specific, which is the point — mcpHandler stands in for
// whatever an SDK assembled, and a REST API takes exactly the same guard.
func ExampleNewVerifier() {
	// The authorization server, in this process, so a revoked token stops
	// working now rather than in fifteen minutes.
	srv, err := oauth2server.NewServer("https://auth.example", memory.NewStore(),
		oauth2server.SubjectAuthenticatorFunc(
			func(context.Context, *http.Request) (*oauth2server.Subject, error) {
				return &oauth2server.Subject{ID: "user_1"}, nil
			}),
		oauth2server.WithScopes("recipes:read", "recipes:write"),
		// The same string the resource names itself by, below. Two spellings of
		// one identifier, and a token's audience is compared against it.
		oauth2server.WithResources("https://api.example/"))
	if err != nil {
		panic(err)
	}

	// What this resource server is, and where its tokens come from. A client
	// that has never heard of either reads this document and finds out.
	meta, err := oauth2server.NewResourceMetadata("https://api.example/",
		[]string{srv.Issuer()},
		oauth2server.WithResourceName("Recipes MCP"),
		oauth2server.WithResourceScopes("recipes:read", "recipes:write"))
	if err != nil {
		panic(err)
	}

	guard, err := oauth2server.NewVerifier(meta, srv)
	if err != nil {
		panic(err)
	}

	// Whatever the MCP SDK assembled from twenty-five tool registrations. It is
	// an http.Handler, so this is the whole of the mount.
	mcpHandler := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		// A tool that writes where the others read asks for itself, against the
		// token the guard already looked up.
		token, _ := oauth2server.TokenFromContext(req.Context())
		if !token.HasScopes("recipes:write") {
			res.WriteHeader(http.StatusForbidden)

			return
		}

		res.WriteHeader(http.StatusOK)
	})

	// srv.Mount(router) puts the six OAuth endpoints on; meta.Mount(router)
	// publishes the document; this is the resource itself.
	protected := guard.Middleware("recipes:read")(mcpHandler)

	// A client that was never configured with this server gets told where to
	// look rather than simply refused.
	res := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/mcp", http.NoBody)
	protected.ServeHTTP(res, req)

	fmt.Println(res.Code)
	fmt.Println(res.Header().Get("WWW-Authenticate"))

	// Output:
	// 401
	// Bearer resource_metadata="https://api.example/.well-known/oauth-protected-resource"
}
