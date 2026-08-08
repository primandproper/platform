/*
Package http adapts requestsigning to inbound HTTP.

It is a routing.Middleware that verifies a request's signature before the
handler runs and answers 401 when it does not check out — the inbound
counterpart to httpclient.WithRequestSigning, over the same
requestsigning.Verifier, so one configured scheme and one key source govern both
directions.

	keys, err := requestsigning.NewSecretKeySource(secretSource, "CALLBACK_KEY", "CALLBACK_KEY_PREVIOUS")
	if err != nil {
		return err
	}

	verifier, err := requestsigning.NewVerifier(keys)
	if err != nil {
		return err
	}

	mw, err := requestsigninghttp.NewMiddleware(verifier,
		requestsigninghttp.WithMetricsProvider(pillars.Metrics))
	if err != nil {
		return err
	}

	routing.Post(router, "/callbacks/payments", handler, routing.WithMiddleware(mw))

# Per route, not globally

Unlike the rate-limiting middleware, this one reads the body: a signature covers
bytes, so the bytes have to be in hand before the handler runs. Installing it
with Router.Use would make every upload route in the service pay for that.
Install it on the endpoints that are actually signed.

The handler is then handed the buffered bytes rather than the socket, with
GetBody set alongside. That is load-bearing. A handler that re-read the
connection, or decoded and re-encoded before acting, would be acting on
something other than what the signature covered — which is the single most
common way a correct scheme is deployed incorrectly.

# The body cap

Verification requires buffering, and an unauthenticated caller chooses how much.
DefaultMaxBodySize caps it at one mebibyte; a body past the cap is rejected
unverified, as a 401 rather than a 413, because that is what happened and
because a distinct status would tell a prober where the cap sits.

WithMaxBodySize raises it for an endpoint that legitimately receives large
signed payloads. There is no unlimited setting.

# Other schemes

The middleware holds a requestsigning.Verifier, not a keyring, so a scheme this
platform did not design — a proof in another header, in another format — is an
implementation of that interface rather than a second copy of this middleware.
Construct one and pass it here; the verifier locates its own proof on the
request, so nothing in this package is specific to v1.

What this package does supply, whatever the scheme, is the bound: the body is
read once and capped here, and the verifier is handed a request whose GetBody
replays those capped bytes. A verifier cannot read past the cap, and cannot
verify bytes the handler will not see.

# What it does not do

It fails closed and cannot be configured otherwise. The rate limiter has a
fail-open default because a limiter that cannot reach Redis is a fault in a
guard rather than a verdict from it; there is no equivalent here. A signature
that did not verify has not verified, and the only thing "letting it through
anyway" would buy is an endpoint that is authenticated on paper.

It also says nothing about *who* signed. One keyring is one counterparty; a
service with many needs a KeySource that resolves per caller, which is what
requestsigning.KeySourceFunc is for.

# The wire shape

Rejections render through routing.DefaultErrorBody: the platform APIError
envelope with code E117, exactly what the Router produces for a handler that
returned requestsigning.ErrInvalidSignature. A service that replaced that
envelope passes its own encoder to WithErrorEncoder — the same one it gave the
Router — so a 401 arrives in the shape its clients already parse.

# Watching it

Three counters: requestsigning_http_verified, requestsigning_http_rejected, and
requestsigning_http_errors. The third is the one to alert on. Rejections are a
guard doing its job and rise whenever a counterparty misconfigures itself;
errors mean this middleware could not reach a verdict at all — a key source it
could not read, a body it could not buffer — and that is a fault in the service
doing the verifying.
*/
package http
