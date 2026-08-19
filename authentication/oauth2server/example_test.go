package oauth2server_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/primandproper/platform-go/v12/authentication/oauth2server"
	"github.com/primandproper/platform-go/v12/authentication/oauth2server/memory"
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
