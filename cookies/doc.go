/*
Package cookies encodes values into cookies that a browser can hold and this
service can later trust, and builds the *http.Cookie carrying them.

# What the cookie actually protects

Two keys are required, both base64 in config, and they do different jobs. The
hash key authenticates: the encoded value carries an HMAC-SHA256 over the
cookie's name, a timestamp, and the payload, so a tampered cookie fails to
decode. The block key encrypts the payload with AES, so the value is opaque to
the client as well. Because the name is inside the MAC, a cookie's value does not
verify under a different name.

That is confidentiality and integrity, not revocation. A cookie remains valid for
as long as its MAC verifies and its timestamp is inside the configured lifetime,
so nothing here can withdraw one that has already been issued — a session that
must be revocable needs server-side state, and the sessions package is where that
lives.

Config.Lifetime bounds the MAC-protected timestamp as well as the cookie's own
MaxAge and Expires. Leaving it unset does not mean "no limit": the underlying
codec then falls back to its own thirty-day ceiling, which is almost never what
the rest of the configuration intends.

# Key handling

The block key must be a valid AES key length — 16, 24, or 32 bytes decoded — and
NewCookieManager does not check it. A wrong-sized key builds a manager without
complaint and fails on the first Encode or Decode instead, so validate lengths
where the keys are loaded if a startup failure is preferable to a runtime one.

There is one key pair, not a ring. Rotating either key invalidates every
outstanding cookie at once, and there is no window in which cookies signed under
the old key still verify.

# The attributes are not optional

BuildCookie always sets HttpOnly and Path=/, and takes Secure, Domain, SameSite,
and the expiry from config. SameSite defaults to Lax, and configuring None is
rejected unless SecureOnly is also set, because browsers silently drop a
SameSite=None cookie that is not Secure — a validation error at startup is the
only place that failure is visible.
*/
package cookies
