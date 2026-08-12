/*
Package jwt is the HS256 tokens.Issuer: JSON Web Tokens signed with a shared
secret.

# What HS256 commits you to

The signing key is symmetric. Verifying a token and minting one are the same
capability, so every service given the key to check tokens can also issue them —
if that is not what you want, the choice to make is a different algorithm, not a
different deployment topology. An empty key is refused at construction, because
HS256 will happily sign and verify under a zero-length HMAC key and anyone who
notices can forge a token.

A JWT's payload is base64, not ciphertext. Every claim is readable by anyone
holding the token, so extraClaims is for data a bearer may see: an account ID is
fine, an internal flag that reveals more than the holder should know is not. The
paseto sibling encrypts instead, and is the choice when the claims themselves are
sensitive.

# What parsing enforces

The parser accepts HMAC signing methods only, so a token presenting a different
alg — "none" included — is rejected before its signature is consulted rather than
verified under an algorithm the attacker chose. Beyond that it requires exp to be
present, and checks aud and iss against the values the Signer was built with.

Minted tokens are backdated one minute in nbf, which buys that much clock skew
between the issuer and any verifier. IssueToken given a non-positive expiry uses
ten minutes rather than minting a token that never expires.

Claim-validation failures come back as the tokens package's sentinels, not
golang-jwt's, with the library's own error still wrapped underneath.
*/
package jwt
