# oauth2diff

A differential harness that drives identical OAuth 2.1 flows against
`authentication/oauth2server` and `github.com/go-oauth2/oauth2/v4`, and holds
them to the same answers.

## Why

`authentication/oauth2server` is a from-scratch implementation of a protocol
that is too large to audit by reading, written for a repository where nobody
claims to be an OAuth expert. "It looks careful" is not evidence, and neither is
"the alternative is widely deployed".

What *is* evidence is agreement. The two implementations overlap on `/authorize`
and `/token`; where they overlap they should answer the same way, and every
place they do not is either a defect here or a divergence somebody has to
justify in writing. This turns "is it compliant?" — which needs a spec expert —
into "do they agree, and where they don't, which one does the RFC back?" — which
needs one afternoon and a copy of RFC 6749.

## Running it

```
cd .conformance/oauth2diff && go test -race ./...
```

Separate module on purpose. It depends on `go-oauth2`, and a test-only
dependency in the root `go.mod` is still a floor every consumer of this library
has to satisfy — the same reasoning that keeps gremlins a pinned container image
rather than a tool directive. Nothing here is importable from the module under
test.

## What the current run establishes

Seven flows, where the two agree exactly:

| flow | both answer |
|---|---|
| authorization code grant | `200` access + refresh |
| authorization code replayed | `invalid_grant` |
| wrong PKCE verifier | `invalid_grant` |
| missing PKCE verifier | `invalid_request` |
| unknown authorization code | `invalid_grant` |
| wrong client secret | `invalid_client` |
| `redirect_uri` mismatch at `/token` | `invalid_grant` |

Four where they differ. In each, the RFC backs this package and not the library:

| flow | oauth2server | go-oauth2 | who is right |
|---|---|---|---|
| unsupported grant type | `400 unsupported_grant_type` | `403 access_denied` | RFC 6749 §5.2 names `unsupported_grant_type`. `access_denied` tells a client its user refused |
| unknown client at `/token` | `401 invalid_client` | `500 server_error` | RFC 6749 §5.2 names `invalid_client`. The library's `ClientStore` reports "unknown" and "storage broke" as the same error |
| unregistered `redirect_uri` | refused | **code issued** | `DefaultValidateURI` compares hosts and never reads the registered path |
| implicit `response_type` | error via the redirect | error in the browser | RFC 6749 §4.1.2.1 sends it to the redirect URI once that URI is validated |

Two divergences measured directly rather than compared:

- **`TestRedirectURIHostSuffix`** — `manage.DefaultValidateURI` is
  `strings.HasSuffix(redirect.Host, base.Host)`, so a client registered for
  `127.0.0.1` also receives authorization codes at `evil127.0.0.1`. This package
  matches the registered string exactly.

- **`TestConcurrentRedemption`** — eight concurrent redemptions of one code.
  This package grants exactly one, every run. The library granted more than one
  in 2 of 10 full-suite runs under `-race` (twice on one occasion, three times
  on another) against its *own in-memory store*; run in isolation it never
  did. `getAndDelAuthorizationCode` reads and then deletes, and `RemoveByCode`
  returns only an `error`, so no store implementation can report which caller
  won.

## What this does not cover

Agreement is only evidence about the overlap, and the overlap is roughly
`/authorize` and `/token`. Dynamic client registration, RFC 8414 discovery, RFC
7009 revocation and RFC 9728 protected resource metadata have no counterpart in
the library — they are the four endpoints `#245` exists for, and nothing here
says anything about them. They are covered by the package's own tests and, for
the flow that matters, by running the MCP Inspector against a deployed instance.

Status codes are reported but not compared: RFC 6749 §5.2 leaves `invalid_client`
free to be 400 or 401, and holding two implementations to the same choice would
be inventing a requirement.
