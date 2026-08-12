/*
Package hashing is the seam for reducing content to a digest, so that which
algorithm computes it can be a runtime choice.

Implementations live in subpackages and vary in what they guarantee, which is the
thing to get right before picking one. sha256 and sha512 are cryptographic
digests. adler32, crc64, and fnv are checksums: fast, fine for detecting
accidental corruption or spreading keys across buckets, and no obstacle at all to
someone constructing a second input with the same digest. hmac is keyed, and
therefore not interchangeable with any of them — two hmac hashers built from
different keys disagree on every input by design, and their output is compared
with hmac.Equal rather than through a hex rendering and ==, which is not
constant-time.

None of these is a password hasher. A digest is fast on purpose, and the property
a stored password needs is the opposite one; use authentication/argon2.

Hash returns raw bytes rather than an encoding of them, and Hex and HexString
render them for the common case. There is no error return: every implementation
here wraps a hash.Hash, whose Write is documented never to fail, so an error
would be an unreachable branch at every call site.

The interface fits a hasher that digests one in-memory buffer. Anything needing
streaming, Size, or BlockSize wants a hash.Hash directly rather than a wider
interface here.
*/
package hashing
