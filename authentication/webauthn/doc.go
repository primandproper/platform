/*
Package webauthn provides passkey registration and login over
github.com/go-webauthn/webauthn, and the ceremony store that makes it work on
more than one replica.

# The ceremony is stateful, and that state is the hard part

A WebAuthn ceremony spans two round trips. The server issues a challenge, the
authenticator signs it, and the server verifies the signature against the
challenge it issued — which means the challenge, and the rest of the
[SessionData] beside it, has to still be there when the second request arrives.
The reference examples for every Go WebAuthn library keep that in a map or in a
cookie-backed session, and both are wrong the moment there is a second replica:
the challenge is issued by one instance and verified by another, and the login
fails intermittently in a way that looks like a client bug.

[SessionStore] is that state, behind an interface with two implementations —
authentication/webauthn/database, which is a table, and
authentication/webauthn/cache, which is a cache.Cache. Both survive a second
replica as long as what backs them does; a memory cache does not, and its doc
says so.

# The user is yours

This package names no user type. [User] is go-webauthn's own interface — a
handle, a name, a display name, and the credentials — and adapting an
application's user to it is twenty lines the application writes, because only
the application knows where its credentials are stored. What is here is the
protocol half: the ceremonies, their state, and the deadline that bounds them.

Storing credentials is likewise the application's: a [Credential] returned by
FinishRegistration is a value to persist, and the sign count on the one
returned by FinishLogin is a value to write back.

# One deadline

Config.CeremonyTimeout is the only expiry knob, and it lands in three places
that would otherwise be three settings able to disagree: the timeout the
browser is asked to honor, the expiry the library enforces server-side when it
verifies, and the TTL the ceremony's row is stored under. A ceremony that has
run out of time therefore fails the same way wherever it is noticed.

# A challenge is used once

[SessionStore.Consume] fetches and removes in one operation, so an assertion
cannot be replayed inside the ceremony window by sending it twice. That is the
interface's whole reason for having Consume rather than a Get and a Delete: a
store that hands the same challenge to two callers is a store that has to be
remembered about, and this one cannot be forgotten.

# Usage

	store, err := webauthndatabase.NewSessionStore(&webauthndatabase.Config{}, db,
		webauthndatabase.WithSweeper(ctx, 5*time.Minute))
	// ...

	rp, err := webauthn.NewRelyingParty(ctx, &webauthn.Config{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
	}, store)
	// ...

	// Registration, first request.
	creation, err := rp.BeginRegistration(ctx, user)
	// ... write creation to the response.

	// Registration, second request.
	credential, err := rp.FinishRegistration(ctx, user, req)
	// ... store credential against the user.

authentication/webauthn/config assembles all of that from environment
configuration, and registers it with a do.Injector — over a cache. The provider
string that chooses between a cache and the SQL table above lives one level down
from the table, in authentication/webauthn/database/config, and that package's
doc.go says why.
*/
package webauthn
