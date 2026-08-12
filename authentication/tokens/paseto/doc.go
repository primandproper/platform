/*
Package paseto is the PASETO v2.local tokens.Issuer: tokens whose claims are
encrypted rather than merely signed.

# What v2.local commits you to

The token is sealed with XChaCha20-Poly1305 under a symmetric key, so the claims
are opaque to whoever holds the token — the difference from the jwt sibling,
whose payload is base64 and readable by anyone. That makes this the choice when a
claim is something the bearer should not be able to read.

Symmetric still means one capability: any holder of the key can mint as well as
verify, exactly as with HS256.

The key must be 32 bytes. NewSigner rejects only an empty one, so a key of some
other length builds a Signer successfully and fails on the first IssueToken with
the cipher's own complaint and the key length in the message. Validate the
length where the key is loaded if you would rather learn about it at startup.

# Claim validation is this package's job, not the library's

Decrypting into a map is authenticated decryption and nothing more: it proves the
token was sealed under this key and has not been altered, and says nothing about
whether it has expired or was meant for this audience. So exp, nbf, aud, and iss
are checked here explicitly, and a token with no exp at all is rejected as
expired rather than treated as eternal. Without that a valid-forever token would
be one the library happily decrypted.

Times survive the payload's JSON round trip as RFC 3339 strings, so claim
extraction accepts both those and the native time.Time a freshly minted payload
holds.

Minted tokens are backdated one minute in nbf for clock skew, and a non-positive
expiry becomes ten minutes. Both match the jwt sibling, which is what lets a
deployment swap one for the other.
*/
package paseto
