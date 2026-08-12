/*
Package tokens is the seam for bearer tokens: an Issuer mints them and parses
them back, and the jwt and paseto subpackages implement it.

# The issuer owns the registered claims

IssueToken sets iss, sub, aud, exp, nbf, iat, and jti itself, and rejects any of
those names in extraClaims with ErrReservedClaim rather than letting a caller
overwrite one. A caller that could set its own exp could mint an immortal token
through the same code path that mints ordinary ones, and the check is what keeps
the token's lifetime a property of the issuer rather than of whoever called it.

Application claims travel in extraClaims and come back through Claims.Get and
Claims.GetString. The typed accessors cover only the claims the issuer owns.

# Parse failures are provider-independent

A token that decodes and authenticates but fails claim validation reports one of
this package's sentinels — ErrTokenExpired, ErrTokenNotYetValid,
ErrInvalidAudience, ErrInvalidIssuer — whichever backend produced it. That is
what makes swapping JWT for PASETO a configuration change: a refresh flow
branching on ErrTokenExpired keeps matching, where it would otherwise silently
stop the moment the underlying library's own sentinel changed.

Implementations translate rather than replace, so the backing library's error is
still reachable underneath for anyone who wants the detail.

# The noop issuer authenticates everything

NoopIssuer mints the empty string and, more importantly, parses anything at all
into empty claims with a nil error. It is a stand-in for a service that issues no
tokens, not a degraded issuer: reaching it from a code path that treats a
successful parse as proof of identity turns every request into an authenticated
one with an empty subject. It has to be named explicitly to be built, and that is
the only thing standing between a misconfiguration and that outcome.
*/
package tokens
