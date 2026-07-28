/*
Package http adapts idempotency to HTTP, on both sides of the wire.

The server half is a routing.Middleware keyed off an Idempotency-Key header.
The client half is an http.RoundTripper that sends the key. They live together
so the header name is one constant rather than two that can drift.

# Server

	manager, err := idempotencyhttp.NewManager(store, locker)
	if err != nil {
		return err
	}

	mw, err := idempotencyhttp.NewMiddleware(manager,
		idempotencyhttp.WithPrincipalExtractor(principalFromRequest),
	)
	if err != nil {
		return err
	}

	routing.Post(router, "/charges", createCharge, routing.WithMiddleware(mw))

Install it per route. It caps request bodies in order to fingerprint them, and
a global Router.Use would impose that cap on upload routes that never asked for
it. Requests without the header pass through with their body untouched, so
opting in is the only way to be affected.

Use NewManager rather than idempotency.NewManager directly. The core package
records every result, which is right for something that knows nothing about
status codes and wrong here: a 500 recorded once would replay for the whole
TTL. NewManager applies Recordable, which draws the line at 5xx.

# Client

	c := httpclient.NewHTTPClient(cfg)
	c.Transport = idempotencyhttp.NewTransport(c.Transport)

	ctx, _ := idempotency.WithNewKey(ctx)   // once, OUTSIDE the retry loop
	err := policy.Do(ctx, func(ctx context.Context) error {
		res, err := c.Do(req.Clone(ctx))
		...
	})

The transport sends the key the context carries and never invents one — see
NewTransport for why that restraint is the whole point.

Two things it deliberately does not do. It does not buffer request bodies, so a
retried request needs a replayable one: rebuild it per attempt, or rely on
http.Request.GetBody, which http.NewRequest fills in for the common body types.
And it does not hoist the key out of the retry loop for you; only the caller
knows where a logical operation begins.

# What a duplicate gets back

A completed record replays its status, its allowlisted headers, and its body,
marked with Idempotent-Replayed. A request that arrives while the first is
still running gets 409 with Retry-After. A key presented with a different
request gets 422.

Replay is close but not byte-exact, in two documented ways.

Headers are an allowlist, defaulting to Content-Type alone. This middleware
runs inside the standard stack, so CORS, request IDs, and trace headers are
reapplied fresh by outer middleware on the replay; replaying the stored copies
would stamp the replay with the original request's trace. A stored Set-Cookie
would be worse, re-setting a session that has since moved on.

Bodies over WithMaxResponseBytes are dropped. The status still replays, so the
effect does not repeat, and the response carries Idempotency-Body-Omitted so
the client is not misled about the empty body. Watch that header's counter and
raise the cap if it fires.

# The fingerprint

Method, path, sorted query, principal, and body hash, so one key cannot answer
two different requests. The principal comes from WithPrincipalExtractor: there
is no platform-wide notion of a caller to read from, and without one two users
sending the same key for the same request would share a record.

The body is hashed as raw bytes. A client that re-serializes its JSON between
attempts changes the fingerprint and is told it reused its key — strict, but
the safe direction to err in. WithFingerprint is there for callers who would
rather canonicalize.

# Recording

Anything short of 5xx is recorded. A 4xx is a stable answer, so replaying it is
correct and cheaper than running the handler again. A 5xx is not: it usually
means the work never landed, so the claim is released and the retry runs.

The cost is the one hole in the guarantee — a handler that has its effect and
then fails will repeat that effect on retry. Recording 5xx would trade that
rare case for a common and worse one, where a transient failure is pinned for
the whole TTL and the client can never succeed.

The response reaches the client as the handler writes it, before the record is
stored. That ordering is deliberate: returning the handler's real answer is
what stops the client retrying at all. When the record then fails to store,
idempotency_record_failures fires and the next retry runs the handler again.
*/
package http
