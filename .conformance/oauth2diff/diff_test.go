package oauth2diff

import (
	"net/url"
	"sync"
	"testing"
)

// callbackURI is the client's registered redirect URI. Loopback http, because
// that is what both servers accept without TLS and what a native client
// actually uses.
const callbackURI = "http://127.0.0.1:61234/callback"

// verdict is the part of an outcome the two servers are held to agree on.
//
// Status codes are reported but not compared: RFC 6749 §5.2 leaves invalid_client
// free to be 400 or 401, and holding two implementations to the same choice
// would be inventing a requirement. What the RFC does pin down is whether a
// grant was issued and, if not, which error code says why.
type verdict struct {
	errorCode string
	granted   bool
	issued    bool
}

func verdictOf(o outcome) verdict {
	code := o.Error
	if code == "" {
		code = o.Redirect
	}

	if code == "" && o.Browser {
		code = "answered-in-browser"
	}

	return verdict{granted: o.HasAccessToken, issued: o.CodeIssued, errorCode: code}
}

// diffCase is one flow run identically against both servers.
type diffCase struct {
	run func(t *testing.T, tg *target) outcome

	name string

	// diverges, when non-empty, says why the two servers are expected to
	// answer differently. Such a case fails if they agree — a divergence that
	// quietly disappeared means the reasoning recorded here is now wrong, and
	// that is worth a red build rather than a silent pass.
	diverges string
}

// happyPath drives authorize → token and returns the token response.
func happyPath(t *testing.T, tg *target) outcome {
	t.Helper()

	verifier, challenge := pkcePair(t)

	code, out := tg.code(t, tg.authorizeQuery(challenge))
	if code == "" {
		return out
	}

	return tg.redeem(t, tg.tokenForm(code, verifier))
}

func cases() []diffCase {
	return []diffCase{
		{
			name: "authorization_code_grant",
			run:  happyPath,
		},
		{
			name: "code_replayed",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				verifier, challenge := pkcePair(t)

				code, out := tg.code(t, tg.authorizeQuery(challenge))
				if code == "" {
					return out
				}

				if first := tg.redeem(t, tg.tokenForm(code, verifier)); !first.HasAccessToken {
					t.Fatalf("%s: first redemption failed: %s", tg.name, first)
				}

				return tg.redeem(t, tg.tokenForm(code, verifier))
			},
		},
		{
			name: "wrong_code_verifier",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				_, challenge := pkcePair(t)
				otherVerifier, _ := pkcePair(t)

				code, out := tg.code(t, tg.authorizeQuery(challenge))
				if code == "" {
					return out
				}

				return tg.redeem(t, tg.tokenForm(code, otherVerifier))
			},
		},
		{
			name: "missing_code_verifier",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				_, challenge := pkcePair(t)

				code, out := tg.code(t, tg.authorizeQuery(challenge))
				if code == "" {
					return out
				}

				form := tg.tokenForm(code, "")
				form.Del("code_verifier")

				return tg.redeem(t, form)
			},
		},
		{
			name: "unknown_code",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				verifier, _ := pkcePair(t)

				return tg.redeem(t, tg.tokenForm("no-such-code", verifier))
			},
		},
		{
			name: "wrong_client_secret",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				verifier, challenge := pkcePair(t)

				code, out := tg.code(t, tg.authorizeQuery(challenge))
				if code == "" {
					return out
				}

				form := tg.tokenForm(code, verifier)
				form.Set("client_secret", "not-the-secret")

				return tg.redeem(t, form)
			},
		},
		{
			name: "unknown_client_at_token",
			// RFC 6749 §5.2 names invalid_client. go-oauth2 answers 500
			// server_error, because its ClientStore reports an unknown client
			// as an error and the manager cannot tell that apart from a
			// storage failure — the same read-only interface that has nowhere
			// to put dynamic registration.
			diverges: "go-oauth2 answers 500 server_error where RFC 6749 §5.2 names invalid_client",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				verifier, challenge := pkcePair(t)

				code, out := tg.code(t, tg.authorizeQuery(challenge))
				if code == "" {
					return out
				}

				form := tg.tokenForm(code, verifier)
				form.Set("client_id", "no-such-client")

				return tg.redeem(t, form)
			},
		},
		{
			name: "redirect_uri_mismatch_at_token",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				verifier, challenge := pkcePair(t)

				code, out := tg.code(t, tg.authorizeQuery(challenge))
				if code == "" {
					return out
				}

				form := tg.tokenForm(code, verifier)
				form.Set("redirect_uri", "http://127.0.0.1:61234/elsewhere")

				return tg.redeem(t, form)
			},
		},
		{
			name: "unsupported_grant_type",
			// RFC 6749 §5.2 names unsupported_grant_type for a grant the
			// server does not support. go-oauth2 answers 403 access_denied,
			// which tells a client its user refused rather than that it asked
			// for something this server does not do.
			diverges: "go-oauth2 answers access_denied where RFC 6749 §5.2 names unsupported_grant_type",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				return tg.postToken(t, url.Values{
					"grant_type":    {"password"},
					"username":      {subjectUsername},
					"password":      {"hunter2"},
					"client_id":     {tg.clientID},
					"client_secret": {tg.clientSecret},
				})
			},
		},
		{
			name: "implicit_response_type_refused",
			// Both refuse it, which is the part that matters. They differ in
			// where the refusal goes: RFC 6749 §4.1.2.1 sends the error to the
			// redirect URI once that URI is known to be the client's, and
			// go-oauth2 answers in the browser instead because its response
			// type check runs before it has a validated request to redirect
			// with.
			diverges: "go-oauth2 answers in the browser where RFC 6749 §4.1.2.1 sends the error to the redirect URI",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				_, challenge := pkcePair(t)

				query := tg.authorizeQuery(challenge)
				query.Set("response_type", "token")

				_, out := tg.code(t, query)

				return out
			},
		},
		{
			name: "unregistered_redirect_uri",
			// The registered callback is /callback; this asks for a different
			// path on the same host. go-oauth2 issues a code for it, because
			// DefaultValidateURI compares hosts and nothing else — the
			// registered path is never read. OAuth 2.1 §4.1.2.1 requires the
			// redirect URI to match a registered one exactly.
			diverges: "go-oauth2 issues a code for an unregistered path; its default matcher compares hosts only",
			run: func(t *testing.T, tg *target) outcome {
				t.Helper()

				_, challenge := pkcePair(t)

				query := tg.authorizeQuery(challenge)
				query.Set("redirect_uri", "http://127.0.0.1:61234/not-registered")

				_, out := tg.code(t, query)

				return out
			},
		},
	}
}

// TestDifferential runs every flow against both servers and holds them to the
// same answer.
func TestDifferential(t *testing.T) {
	t.Parallel()

	for _, c := range cases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			platform := startPlatform(t, callbackURI)
			library := startLibrary(t, callbackURI)

			platformOut := c.run(t, platform)
			libraryOut := c.run(t, library)

			t.Logf("platform  %s", platformOut)
			t.Logf("go-oauth2 %s", libraryOut)

			same := verdictOf(platformOut) == verdictOf(libraryOut)

			switch {
			case c.diverges != "" && same:
				t.Errorf("expected a divergence and did not get one (%s); both answered %s",
					c.diverges, platformOut)
			case c.diverges == "" && !same:
				t.Errorf("servers disagree:\n  platform  %s\n  go-oauth2 %s", platformOut, libraryOut)
			}
		})
	}
}

// TestRedirectURIHostSuffix measures the library's shipped redirect-URI
// matcher against the platform package's.
//
// This is a divergence rather than a disagreement: manage.DefaultValidateURI
// accepts any host with the registered host as a *suffix*, so a client
// registered for 127.0.0.1 also receives codes at anything ending in it. The
// platform package matches the registered string exactly, as OAuth 2.1
// requires. If this test ever reports agreement, one of the two changed and
// the reasoning here needs rewriting.
func TestRedirectURIHostSuffix(t *testing.T) {
	t.Parallel()

	// A host ending in the registered host, which is a different host.
	const lookalike = "http://evil127.0.0.1:61234/callback"

	platform := startPlatform(t, callbackURI)
	library := startLibrary(t, callbackURI)

	attempt := func(tg *target) outcome {
		_, challenge := pkcePair(t)

		query := tg.authorizeQuery(challenge)
		query.Set("redirect_uri", lookalike)

		_, out := tg.code(t, query)

		return out
	}

	platformOut := attempt(platform)
	libraryOut := attempt(library)

	t.Logf("platform  %s", platformOut)
	t.Logf("go-oauth2 %s", libraryOut)

	if verdictOf(platformOut) == verdictOf(libraryOut) {
		t.Errorf("expected the shipped matchers to differ on %q; both answered %s",
			lookalike, platformOut)
	}
}

// TestConcurrentRedemption redeems one authorization code from several
// goroutines at once and counts the winners.
//
// The invariant is exactly one, and only the platform package is held to it
// here.
//
// go-oauth2's manager reads the code and then deletes it as two separate calls
// (manage/manager.go getAndDelAuthorizationCode), and its TokenStore interface
// has nowhere to report which caller won — RemoveByCode returns only an error.
// Two requests can therefore both pass the read before either delete lands.
//
// This reproduces. Against the library's own in-memory store, under -race and
// with the rest of this suite running in parallel, 2 of 10 runs granted more
// than one token for a single code — twice on one occasion, three times on
// another. Run alone it is 1-in-1 every time, which is the point: the window is
// scheduling-dependent, so a test that redeems one code at a time never sees
// it, and neither does a lightly loaded staging environment. A table, where the
// read and the delete are two round trips with a network between them, holds
// the window open far wider than a mutex-guarded map does.
//
// The library's count is therefore logged rather than asserted — asserting a
// scheduling-dependent failure would make this suite flaky. What is asserted is
// the platform package's invariant, which holds because its store resolves the
// redemption in one guarded write instead of a read followed by a delete.
func TestConcurrentRedemption(t *testing.T) {
	t.Parallel()

	const attempts = 8

	for _, tg := range []*target{startPlatform(t, callbackURI), startLibrary(t, callbackURI)} {
		t.Run(tg.name, func(t *testing.T) {
			verifier, challenge := pkcePair(t)

			code, out := tg.code(t, tg.authorizeQuery(challenge))
			if code == "" {
				t.Fatalf("%s: could not obtain a code: %s", tg.name, out)
			}

			var (
				wg      sync.WaitGroup
				mu      sync.Mutex
				granted int
			)

			wg.Add(attempts)

			for range attempts {
				go func() {
					defer wg.Done()

					result := tg.redeem(t, tg.tokenForm(code, verifier))

					mu.Lock()
					defer mu.Unlock()

					if result.HasAccessToken {
						granted++
					}
				}()
			}

			wg.Wait()

			t.Logf("%s: %d of %d concurrent redemptions were granted a token", tg.name, granted, attempts)

			if tg.name == "platform" && granted != 1 {
				t.Errorf("platform granted %d tokens for one code, want exactly 1", granted)
			}
		})
	}
}
