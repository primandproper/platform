/*
Package requestsigning proves that an HTTP request body was produced by someone
holding a shared key, and that it was produced recently.

It is one implementation of the timestamped-HMAC scheme, for every place the
platform needs one: outbound webhooks, inbound webhooks from a third party, and
first-party service-to-service calls. Those three used to be three
reimplementations, and the failure mode of a reimplemented signing scheme is
that it looks fine until somebody times a comparison.

	X-Platform-Signature: v1,t=1753900000,s=<hex(HMAC-SHA256(key, "v1." + t + "." + body))>
	X-Platform-Timestamp: 1753900000

# The scheme

Both the version and the timestamp are inside the signed material, and each is
load-bearing.

The timestamp is what makes a captured request expire. A signature over the body
alone is valid forever, so anyone who observes one request can replay it
indefinitely. Verify rejects anything outside DefaultTolerance, and does so
before computing any HMAC, so a replay flood costs the receiver nothing.

The v1 prefix is what makes the construction replaceable. Binding the scheme
into the signed bytes means a v1 signature can only ever verify as v1, so a
later scheme can be introduced alongside it rather than by flag-day.

Rotation is the other half. Keyring carries Current and Previous, and a request
is signed under both while Previous is set — several s= components in one
header. Either side rolls its key by accepting both for as long as it needs, and
the operator clears Previous afterwards. A single shared secret cannot be rolled
at all without breaking every counterparty simultaneously; in practice that
means it never gets rolled.

# The three seams

Sign and Verify are the functions, for code that already holds the bytes —
webhooks signs its deliveries through Sign.

Signer and Verifier are the interfaces, for code that should not have to. Both
resolve their keyring per operation through a KeySource, which is what turns a
rotation into a change in the secret store rather than a deploy.

Neither takes an *http.Request. Signer.SignHeaders writes into a header bag and
Verifier.VerifyHeaderValue reads one value out of it; who reads the body, and
what bounds that read, is a serving concern that belongs to the caller — which
is why the request-shaped ergonomics live in requestsigning/http and httpclient
rather than here.

	keys, err := requestsigning.NewSecretKeySource(secretSource, "SIGNING_KEY", "SIGNING_KEY_PREVIOUS")
	if err != nil {
		return err
	}

	signer, err := requestsigning.NewSigner(keys)
	if err != nil {
		return err
	}

	client, err := httpclient.NewHTTPClient(
		httpclient.WithRequestSigning(signer),
		httpclient.WithRetryPolicy(policy),
	)

The signing transport sits *under* the retry loop, so every attempt is signed
afresh. A retry that fires after thirty seconds of backoff carrying the original
attempt's timestamp arrives stale, and the receiver is right to reject it — which
is a failure that only shows up under load, in the requests that were already
having a bad time.

The inbound half is a routing.Middleware in requestsigning/http, over the same
Verifier.

# Keys

Read them through secrets, not config. NewSecretKeySource resolves both names on
every operation, which is affordable because secrets.NewCachingSource answers
from memory and consults the backend once per TTL; pair it with
secrets.WithRefresh so a rotation is noticed on a timer rather than on the next
request.

The secret's value is used as key material verbatim. A store holding it base64-
or hex-encoded should be wrapped in a KeySourceFunc that decodes it, so what the
store holds and what the HMAC consumes cannot drift.

# Other schemes

Verifier is an interface so that a third party's scheme is an implementation of
it rather than a second verification stack. A Stripe or GitHub verifier lives in
its own package, satisfies this interface, and is handed to the same middleware:

	verifier := stripe.NewVerifier(keys)   // hypothetical; see #96

	mw, err := requestsigninghttp.NewMiddleware(verifier)

There is no registry of schemes by name, and that is deliberate. Which scheme
guards an endpoint is something the wiring already knows — it is choosing the
route and the key source in the same breath — so a name-to-constructor map would
buy nothing except package-level mutable state and an ordering dependency
between init functions. A service that genuinely needs to pick from a config
string does what the rest of this module does: a config subpackage with a switch
over the providers it imports, returning errors.ErrUnknownProvider for a name it
does not carry.

Whatever the selection mechanism, it belongs to startup. Reading the scheme off
an incoming request would let the caller choose which verifier judges it, and a
caller who can choose picks the weakest one on offer.

# What the failures mean

ErrInvalidSignature is deliberately undifferentiated — missing header, malformed
header, unknown scheme, wrong key, tampered body are one error, because telling
a caller which one applied tells an attacker how close a forgery came.
ErrStaleSignature is separate, because clock skew is the one benign cause and an
operator can act on it. Both map to 401 through errors/http, and to
codes.Unauthenticated through errors/grpc.

ErrNoVerificationKey is neither: a verifier holding no keys rejects everything,
which looks identical to a fleet of callers that all got their signing wrong.
It is unmapped, so it surfaces as a 500 — the server's fault, reported as the
server's fault.
*/
package requestsigning
