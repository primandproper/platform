/*
Package oauth2server is an OAuth 2.1 authorization server: the endpoints, the
grant logic, and a Store seam underneath them.

	store, _ := oauth2database.NewStore(&oauth2database.Config{}, db)

	srv, _ := oauth2server.NewServer("https://auth.example", store, authenticator)
	srv.Mount(router)

It exists because a resource server whose clients discover it at runtime and
hold no pre-registered client identifier has to be an authorization server —
protected resource metadata (RFC 9728), authorization server metadata (RFC
8414), authorization code with PKCE, and dynamic client registration (RFC 7591)
— and that is a lot of protocol to assemble out of the reference examples. The
examples are map-backed, so every assembly from them is map-backed, and a
map-backed authorization server works exactly until there are two replicas.

None of this is specific to any one protocol built on top of it. The same
endpoints serve any RFC 7591 client.

# What you have to supply

Two things, and they are the two this package must not decide.

A SubjectAuthenticator says who the human at /authorize is. One deployment
checks a username, an argon2 password, and a TOTP code against its own identity
repository; another delegates to a corporate identity provider. There is no
default, because a default would be a server that issues authorization codes to
whoever asks.

A Subject says what a token means. ID is the "sub"; Claims is whatever the
resource server needs beside it — an account identifier, a tenant, a role — and
this package neither reads it nor constrains it beyond requiring strings.

Everything else has a default, the login form included: see DefaultLoginRenderer
for a plain page that works with no stylesheet and no assets, and
WithLoginRenderer for replacing it.

# The resource owner who is already signed in

A form is the right answer for a browser and the wrong one for everything else.
A first-party application holding a session cookie, a CLI holding a token, a
service exchanging one credential for another — none of them has anything to
type, and the only thing they can do with a login page is fail to parse it.

WithSubjectResolver registers a seam consulted before the form is rendered and
before SubjectAuthenticator is asked, on GET and POST alike. A request carrying
proof of who its owner is redirects with an authorization code; a request
carrying none, or one the resolver does not recognize, gets the form exactly as
before. A Server built without one is unchanged.

Two mechanisms, two seams, rather than one method that inspects the request and
forks. Which of "presented a credential" and "typed a password" wins is a
protocol question — the answer here is that proof already held wins — and
folding both into SubjectAuthenticator leaves every deployment to answer it
again, slightly differently, in application code. It also removes the reason a
machine client had to POST: an empty body sent to a URL whose parameters are all
in the query string was never anything but an artifact of where the seam was.

# The endpoints

	GET  /.well-known/oauth-authorization-server   RFC 8414 discovery
	GET  /authorize                                the login form, or a code for an
	                                               already-authenticated owner
	POST /authorize                                authenticate, issue a code
	POST /token                                    authorization_code, refresh_token
	POST /register                                 RFC 7591 dynamic registration
	POST /revoke                                   RFC 7009 revocation

Mount registers all six on a routing.Router; Handler returns them as one
http.Handler for a caller not using routing. The seventh document — RFC 9728
protected resource metadata — is emitted by the resource server, which is not
necessarily this process, so it is a separate mountable thing: ResourceMetadata.

/register is the one of the six a deployment can turn off, for one whose clients
are administered somewhere else — created through a permission-gated API, seeded
by a migration — and for which an anonymous endpoint writing to the same client
table would be a way around those permissions. WithDynamicRegistration(false)
takes it off the router and out of the discovery document in the same breath,
because a document naming an endpoint that 404s is the failure the document
exists to avoid.

It is also the one of the six an anonymous caller can write rows through, so a
deployment that keeps it wants a bound on how fast. This package will not guess
what the bound is per: who a caller *is* depends on a deployment's proxy,
gateway, and address handling, and an address read from in here is a load
balancer's as often as a client's. So the decision is a deployment's and the
seam is WithRegistrationLimiter, which takes the gate it built —
ratelimiting/http.NewMiddleware over any ratelimiting.RateLimiter is the
expected one — and wraps RegisterHandler in it. Wrapping the handler rather than
taking middleware at Mount is what makes it survive the router: Handler, Mount,
and a deployment routing the endpoint by hand are bounded by the same
construction call, and a deployment that names no gate gets exactly what it got
before.

/revoke answers the same empty 200 whether it revoked a session or was handed a
token nobody ever issued — RFC 7009 §2.2 requires that, so a client cannot use
it to find out which tokens exist. A deployment often needs to know anyway, to
emit its own "this user signed out" event, and asks with WithRevocationObserver
rather than by inspecting a response that deliberately carries nothing.

# The resource server's half

Everything above mints tokens. Verifier is what a protected resource does with
one, and it is a separate type for the same reason ResourceMetadata is: the
resource server and the authorization server are frequently not the same
component even when they are the same process.

	guard, _ := oauth2server.NewVerifier(metadata, srv)
	router.Handle(http.MethodPost, "/mcp", mcpHandler,
		guard.Middleware("recipes:read"))

Three checks, and the middle one is why this exists.

The token is live: delegated to Server.Authenticate, which reads the Store. The
token names this resource: its RFC 8707 audience is compared against the
resource identifier in the metadata document. The token carries the scopes the
route asked for.

Authenticate is the first of those three and only the first. It answers "is this
a live token", because it is the authorization server's lookup and the
authorization server serves every resource behind it — so a deployment running
two protected resources against one authorization server has a cross-resource
replay the moment either of them reads a non-nil return as an authorized
request. The audience comparison is the one a resource server has to make for
itself, and the only thing it needs in order to make it is its own identifier:
naming that is what building a ResourceMetadata already is, so a Verifier is
built from one rather than asking for the string a second time.

A token that carries no audience at all is refused, and there is no option that
accepts one. It is the token RFC 8707 exists to prevent; a deployment where this
refuses everything has clients that are not sending the resource parameter.

Verify is the same three checks without the HTTP, for a resource server that
writes its own response envelope, and WriteChallenge writes the RFC 6750
refusal — status and WWW-Authenticate — for one that only wants the header
right. A handler underneath Middleware reads what was verified with
TokenFromContext instead of looking it up again.

# The decision with the most reach: what an access token is

An access token here is opaque, and every resource-server request that carries
one costs a lookup in the Store. The alternative — a signed
authentication/tokens JWT or PASETO, verified locally — is a real option that
this package deliberately does not take, and the reasoning is worth having in
the open rather than inherited from whichever was written first.

A signed token makes verification free and revocation impossible. What carries
revocability then is the refresh token, which means a sign-out ends the session
at the end of the access token's lifetime rather than now — so the access token
lifetime becomes a direct trade against how long a revoked session keeps
working, and the pressure is always to make it shorter, which puts the load back
on the token endpoint.

An opaque token makes revocation immediate and verification a lookup. The Store
is required either way, for authorization codes and client registrations, so the
opaque choice adds no dependency; it adds a query on a primary key, on a table
whose live rows are bounded by (sessions × access token lifetime).

Immediate revocation is the property this package is unwilling to give up. It
ships a /revoke endpoint, and an endpoint that answers 200 to "end this session"
and then leaves the session working for another quarter of an hour is worse than
not shipping one. Server.Authenticate is how an in-process resource server does
the lookup.

The cost is stated plainly rather than hidden: a resource server in a different
process cannot call Authenticate, and this package ships no RFC 7662
introspection endpoint for it to call instead. Share the Store, or hold the
resource server in the same process.

# The access token lifetime

Fifteen minutes, and it is the number this package most deliberately did not
inherit. The examples this generalizes use twenty-four hours, and they use it
because their store is a map: a restart would otherwise sign everybody out, so
the token had to outlive the process. With a durable store and a rotating
refresh token behind it, a long-lived bearer token buys nothing — the session
already survives — and costs the entire window in which a leaked one works.

# What is checked, and why each one is not a follow-up

Three of these are the difference between an authorization server and something
that looks like one, and all three are cheap while the interface is being drawn
and awkward afterwards.

Redirect URIs are matched exactly, byte for byte, against the ones the client
registered — at /authorize, and again at /token against the URI the code was
issued for. Not a prefix, not "same host", not "ignoring the query string".
Where the registered URIs are stored and never read again, any redirect_uri a
request supplies receives an authorization code; PKCE keeps whoever receives it
from redeeming it, so that is a leak rather than a takeover, but registration
was supposed to be what stopped it.

Client secrets are verified at /token, against the registration rather than
against whatever the authorization code recorded. A metadata document that
advertises client_secret_post from an endpoint reading no secret makes
registration decorative.

Tokens carry an audience, from RFC 8707 resource indicators, so that a token
minted for one resource server cannot be replayed at another. A resource server
that finds its own identifier absent must refuse the token; this package cannot
make that check on its behalf.

Every credential is stored as a hex SHA-256 digest and never as itself, so a
database dump contains nothing redeemable — invisible in a map that dies with
the process, and the entire difference once the store is a table.

PKCE is mandatory and S256-only. There is no option to disable it and no support
for the "plain" method, which puts the verifier in the request PKCE exists to
protect.

Refresh tokens rotate and reuse is detected. Rotation alone is bookkeeping: the
replayed token is refused and the copy the attacker is using keeps working, so
the theft leaves one failed request and no other trace. What makes it worth
doing is that a replay revokes the whole family — see RefreshToken.FamilyID.

A replayed authorization code revokes its family too, which is RFC 6749 §4.1.2
and is the same threat one step earlier: whoever wins the race to /token keeps a
token pair, and the loser's replay is the only signal that there were two of
them. It works because the family is minted at /authorize and carried on the
code — see AuthorizationCode.FamilyID — rather than at the redemption, which
would leave a replay detectable and unanswerable. Unlike refresh reuse it has no
switch: WithRefreshReuseDetection exists because a client that loses the response
to a rotation and retries revokes a session it is using, and a replayed code
cannot cost that — a client that received the pair has nothing to retry, so what
is revoked is a pair nobody is holding.

# What it does not implement

The implicit grant and the resource-owner-password grant, both removed by OAuth
2.1. There is no option that brings them back, because an option is a thing a
deployment can be misconfigured into.

Token introspection (RFC 7662) and consent screens beyond the login form. The
first is discussed above; the second is application-shaped in the same way the
login form is, and WithLoginRenderer is the seam for it.

An adapter for any one protocol built on top of this, and a dependency on any
one protocol's SDK. A remote MCP server is the case that prompted asking, and it
is worth saying where the line fell, because "the machinery is generic and the
tools are not" is true and still does not put an mcp package in this module.

What is generic about an OAuth-protected MCP server is not MCP. It is the
resource server: extract a bearer token, check it is live, check it was minted
for this resource, check its scopes, and answer a refusal with the RFC 9728
pointer that starts a client's discovery. That is Verifier above, and it is the
same code for a REST API behind the same tokens — so a package named mcp would
have been a name a REST resource server either could not reach or had to import
a protocol it does not speak to get at.

What is left after that is genuinely MCP's, and genuinely the consumer's: the
server assembly, the tool registration, the schemas. An MCP server is an
http.Handler, so mounting it is Middleware around it and a Handle call on the
same router. A deployment preferring its SDK's own bearer middleware hands that
middleware a function calling Verify and copying Scopes and ExpiresAt into the
SDK's token type — three lines, against an SDK version this module then never
has to have an opinion about. Taking the dependency, or defining an interface
for a consumer's SDK to satisfy, would both buy those three lines at the price
of a vendor API in this module's go.mod or in its exported surface.

# Choosing a store

oauth2server/database keeps four SQL tables, and is what a deployment wants.
oauth2server/memory keeps four maps, and is for tests and single-process
development. They are held to the same conformance suite,
oauth2server/oauth2servertest, including the two cases that separate them: a
code redeemed twice concurrently, and a record that expires between a read and
the write that follows it.

oauth2server/config assembles the server and the memory store from environment
configuration, with a do.Provide registration. The provider string that chooses
between memory and the tables lives one level down from the tables, in
oauth2server/database/config, and that package's doc.go says why.

# Watching it

	oauth2server_requests                by endpoint: metadata, authorize, token,
	                                     register, revoke, and verify — the last
	                                     being a Verifier rather than an endpoint
	                                     here, sharing the label so one panel
	                                     covers the tokens minted and the
	                                     requests they are spent on.
	oauth2server_errors                  by endpoint and OAuth error code. A rising
	                                     invalid_grant on the token endpoint is
	                                     usually a client with a broken PKCE
	                                     implementation; a rising invalid_client is
	                                     usually a registration that lapsed.
	oauth2server_latency_ms              by endpoint.
	oauth2server_codes_issued            authorization codes. Should track logins.
	oauth2server_tokens_issued           token pairs, from both grants.
	oauth2server_clients_registered      dynamic registrations. This is the one an
	                                     anonymous caller drives, so a spike here
	                                     is the signal that /register wants a
	                                     WithRegistrationLimiter gate — and once
	                                     one is set, the refusals are the gate's
	                                     own counters rather than this one, which
	                                     counts what got through.
	oauth2server_refresh_reuse_detected  replayed refresh tokens and codes. Not
	                                     always an attack — a client that lost the
	                                     response to a refresh and retried lands
	                                     here too — but never nothing.
	oauth2server_revocations             records revoked, by /revoke and by reuse
	                                     detection together.
	oauth2server_audience_rejections     live tokens presented at a Verifier they
	                                     were not minted for. An unknown token is
	                                     background — sessions end all day, and
	                                     those land on the shared error counter
	                                     under endpoint "verify" with
	                                     invalid_token — while a token that is
	                                     good somewhere else arriving here is a
	                                     client pointed at the wrong server or a
	                                     replay, and is never nothing.

No credential appears on a span or in a log line, hashed or otherwise: a hash of
a bearer credential is the store's lookup key for it. The client identifier is
recorded, and is public by construction.
*/
package oauth2server
