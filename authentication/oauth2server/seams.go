package oauth2server

import (
	"context"
	"net/http"
)

// SubjectAuthenticator identifies the human behind an authorization request.
//
// This is one of the places this package deliberately stops. Everything else
// here is protocol — the same protocol for every deployment — and this is the
// application: dinnerdonebetter's is a username, an argon2 password, and a
// TOTP code checked against its own identity repository; another consumer's is
// a corporate identity provider.
//
// It is asked only when nothing else has already answered: it means the human
// typed something. A resource owner who is already authenticated by other
// means — a session cookie, a token a first-party client holds — is
// SubjectResolver's, which is consulted first and needs no form at all.
//
// # What it is handed and what it owes back
//
// It receives the parsed /authorize request, form values included, so it can
// read whatever fields its own login form posts. It returns either the Subject
// the tokens will be minted for, or an error.
//
// An error wrapping ErrLoginFailed re-renders the login form with a message,
// which is the answer to a wrong password: the human is still here and can try
// again. Any other error fails the request, which is the answer to a broken
// identity store — retrying a form against a database that is down produces a
// user who tries four times and then files a support ticket.
//
// Returning (nil, nil) is treated as ErrLoginFailed. A Subject with an empty ID
// is rejected outright: a token whose subject is the empty string authorizes
// whoever the resource server decides the empty string is.
//
// # What it must not do
//
// Write to the ResponseWriter — it does not have one, deliberately. An
// authenticator that could render its own response could redirect somewhere
// this package never validated, which is the redirect it exists to prevent.
type SubjectAuthenticator interface {
	AuthenticateSubject(ctx context.Context, req *http.Request) (*Subject, error)
}

// DefaultLoginFailureMessage is what the form says when a
// SubjectAuthenticator refuses without naming a message of its own.
//
// It is deliberately uninformative about which half was wrong. "No such user"
// and "wrong password" as separate answers make the form an account
// enumeration oracle, and a rate limiter does not fix that — it slows it down.
const DefaultLoginFailureMessage = "Sign-in failed. Check your details and try again."

// LoginError is how a SubjectAuthenticator names the message the login form
// shows.
//
// It exists because the alternative — rendering the error's own text — makes
// every authenticator's internal error message a string in a browser, and the
// author of that message had no reason to think it would be. A *LoginError's
// Message is the only string from an authenticator that reaches a page.
//
// It wraps ErrLoginFailed, so an authenticator returning one gets the re-render
// behavior without also having to say so:
//
//	return nil, oauth2server.NewLoginError("That code has expired.", err)
type LoginError struct {
	// cause is what is recorded, and is never shown.
	cause error

	// Message is shown to the human. Write it for them.
	Message string
}

// NewLoginError builds a LoginError. An empty message renders
// DefaultLoginFailureMessage; a nil cause is fine, and means there was nothing
// underneath worth recording.
func NewLoginError(message string, cause error) *LoginError {
	return &LoginError{Message: message, cause: cause}
}

// Error implements error, rendering what is recorded rather than what is shown.
func (e *LoginError) Error() string {
	if e.cause != nil {
		return "login failed: " + e.cause.Error()
	}

	return "login failed: " + e.Message
}

// Unwrap reports both ErrLoginFailed and whatever the authenticator was
// reacting to, so a caller can match either.
func (e *LoginError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrLoginFailed}
	}

	return []error{ErrLoginFailed, e.cause}
}

// SubjectAuthenticatorFunc adapts a function to SubjectAuthenticator.
type SubjectAuthenticatorFunc func(ctx context.Context, req *http.Request) (*Subject, error)

// AuthenticateSubject implements SubjectAuthenticator.
func (f SubjectAuthenticatorFunc) AuthenticateSubject(ctx context.Context, req *http.Request) (*Subject, error) {
	return f(ctx, req)
}

// SubjectResolver reports the resource owner when the request already carries
// proof of who they are.
//
// It is the seam for the case a login form cannot serve: a first-party
// application holding a session cookie, a CLI holding a token, a service
// exchanging one credential for another. None of those has anything to type,
// and without this seam the only way to reach a subject is to POST a form —
// which is a strange thing to ask of a client whose parameters are all in the
// query string, and true only because that is where the seam used to be.
//
// It is consulted on GET and on POST alike, before the login form is rendered
// and before SubjectAuthenticator is asked anything. A request carrying proof
// therefore never meets a form, and one carrying none behaves exactly as it
// did before — the seam is optional, and a Server built without one is
// unchanged.
//
// # What it owes back
//
// (nil, nil) means "not one of mine": no credential, an expired one, one this
// resolver does not recognize. The request carries on to the form, which is
// the honest answer to "I cannot say who this is" and the reason an expired
// session cookie still sends a browser to sign in again.
//
// A Subject with a non-empty ID issues an authorization code and redirects,
// exactly as a successful form login would. An empty ID is refused: a token
// whose subject is the empty string authorizes whoever the resource server
// decides the empty string is.
//
// An error ends the attempt at the client's registered redirect URI, with no
// form rendered. That is the difference between the two seams, and it is
// deliberate: a caller who presented a credential and had it rejected has
// nothing to type, so sending it a login page would be sending an answer it
// cannot use. Reserve the error for a resolver that is actually broken, or for
// a credential this deployment means to refuse outright — an absent or lapsed
// one is (nil, nil).
//
// # What it must not do
//
// Write to the ResponseWriter, for the same reason SubjectAuthenticator must
// not: it does not have one, and a seam that could render its own response
// could redirect somewhere this package never validated.
type SubjectResolver interface {
	ResolveSubject(ctx context.Context, req *http.Request) (*Subject, error)
}

// SubjectResolverFunc adapts a function to SubjectResolver.
type SubjectResolverFunc func(ctx context.Context, req *http.Request) (*Subject, error)

// ResolveSubject implements SubjectResolver.
func (f SubjectResolverFunc) ResolveSubject(ctx context.Context, req *http.Request) (*Subject, error) {
	return f(ctx, req)
}

// LoginView is what the login form is rendered from.
//
// ClientName is attacker-supplied — it is whatever the registration request
// said, and registration is open — so a renderer must escape it. The shipped
// renderer uses html/template, which does that by construction; a renderer that
// builds HTML by concatenation is choosing to be an XSS.
type LoginView struct {
	// Action is where the form posts: the /authorize URL with the original
	// query string intact. The authorization parameters travel in the query
	// rather than in hidden form fields so that the POST is validated against
	// exactly the same request the GET was.
	Action string

	// Error is the message to show, or empty on the first render.
	Error string

	// ClientName is the registered client_name, or the client_id if the
	// registration named none.
	ClientName string

	// Scopes are what the client asked for, so the human can see what they are
	// approving.
	Scopes []string
}

// LoginRenderer draws the form the resource owner authenticates at.
//
// A default is shipped — DefaultLoginRenderer — because a package that made
// every consumer write one would be shipping seven-eighths of an authorization
// server. It is deliberately plain: an application that cares how its login
// page looks replaces this, and one that does not gets a page that works
// without a stylesheet.
//
// A renderer owns the whole response, status included, which is the one place
// this package hands that over. It has to: a renderer that wanted to answer 429
// on a rate-limited login could not say so otherwise.
type LoginRenderer interface {
	RenderLogin(ctx context.Context, res http.ResponseWriter, view LoginView)
}

// LoginRendererFunc adapts a function to LoginRenderer.
type LoginRendererFunc func(ctx context.Context, res http.ResponseWriter, view LoginView)

// RenderLogin implements LoginRenderer.
func (f LoginRendererFunc) RenderLogin(ctx context.Context, res http.ResponseWriter, view LoginView) {
	f(ctx, res, view)
}

// RegistrationRequest is an RFC 7591 dynamic client registration request, as
// received.
//
// Every field is attacker-supplied. /register is unauthenticated because RFC
// 7591 requires it to be for the discovery flow to work at all, which makes
// vetting this the authorization server's problem rather than an optional
// extra.
type RegistrationRequest struct {
	ClientName              string   `json:"client_name,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
}

// RegistrationPolicy decides whether a registration is accepted.
//
// The default — DefaultRegistrationPolicy — enforces what the protocol cannot
// be safe without: at least one redirect URI, every one of them absolute, https
// or loopback, and free of a fragment. Replace it to add whatever else a
// deployment needs: an allowlist of hosts, a cap tied to a tenant, a check
// against an out-of-band approval.
//
// What it is not is a rate limiter. A policy is handed the parsed metadata and
// not the request, so it cannot see a caller at all — and identifying one
// depends on how a deployment is fronted anyway: source address, a proxy
// header, an API gateway's own token. WithRegistrationLimiter is where that
// answer goes, and ratelimiting/http builds the gate it takes.
type RegistrationPolicy interface {
	// AllowRegistration vets a request. An error wrapping
	// ErrRegistrationRejected renders as a 400 with invalid_client_metadata
	// and the error's message; anything else is a 500.
	AllowRegistration(ctx context.Context, req *RegistrationRequest) error
}

// RevocationObserver is told what a /revoke request actually revoked.
//
// It is a function rather than an interface because there is nothing for a
// deployment to implement here: this is a notification, and the only thing an
// interface would add is a type to declare somewhere.
//
// # When it is called
//
// After a revocation that removed something, with the subject the record
// belonged to and the family it was part of. Never for a token that was already
// gone, never for a token belonging to another client, and never when the store
// refused the write — RFC 7009 §2.2 requires the same empty 200 in all of those
// cases, and this is the difference the response cannot carry.
//
// Revoking a refresh token takes its whole family, so this is called once for
// the family rather than once per record. Revoking an access token names the
// family it belonged to, which is still live: an access token revocation is not
// a sign-out, and a consumer that treats it as one will emit an event for a
// session that is still going.
//
// What does not reach it is a revocation this server decided on: the family
// killed by refresh token reuse detection, or by a refresh token presented by
// the wrong client. Those are not sign-outs — reporting them through the same
// callback would have a deployment logging "user signed out" for a theft — and
// they are already visible as oauth2server_refresh_reuse_detected and a
// recorded operation.
//
// # What it must not do
//
// Block. It runs inline, on the request goroutine, before the 200 is written,
// so a slow observer is a slow sign-out; a deployment publishing a message
// should hand it to whatever it already has for that rather than waiting on a
// broker from in here. The context is the request's, so it is cancelled the
// moment the client hangs up.
//
// A panic is recovered and recorded rather than allowed to take down the
// request: the records are already gone by the time this runs, and a failing
// analytics callback must not turn a sign-out that succeeded into a 500 the
// client retries.
type RevocationObserver func(ctx context.Context, subject Subject, familyID string)

// RegistrationPolicyFunc adapts a function to RegistrationPolicy.
type RegistrationPolicyFunc func(ctx context.Context, req *RegistrationRequest) error

// AllowRegistration implements RegistrationPolicy.
func (f RegistrationPolicyFunc) AllowRegistration(ctx context.Context, req *RegistrationRequest) error {
	return f(ctx, req)
}
