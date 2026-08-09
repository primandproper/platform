/*
Package encryption provides authenticated encryption over a rotatable set of
keys.

The surface is three interfaces and one implementation. Cipher is
single-key authenticated encryption, and it is what a provider implements —
see the aes subpackage. Keyring composes Ciphers into an EncryptorDecryptor
that writes under a current key and reads under any key it still holds.
KeyWrapper is a separate seam for encrypting key material rather than data;
see the kms subpackage.

# Rotation

A ciphertext names the key that produced it, in the clear, at the front of the
frame. That one fact is what makes rotation incremental: adding a key and
naming it current changes what new writes use, and every existing ciphertext
keeps opening under the key it already names. Nothing has to be re-encrypted
at the moment of the change.

Moving old rows over is therefore a background concern rather than a flag day —
re-encrypt on next write, and sweep whatever is never written again. The
keyring counts decryptions per key ID so that sweep has a finish line:
decryptions still attributed to a retired key are exactly the rows that have
not been reached.

The dangerous operation is retiring a key, not adding one. A key dropped from
the ring while ciphertexts still name it makes those rows unreadable, and
permanently so once the material is gone.

# Associated data

Encrypt and Decrypt take associated data alongside the payload: authenticated,
not encrypted, and not recoverable from the ciphertext. Supplying the identity
of the thing being encrypted — a row's primary key, a subject ID, a column
name — binds the ciphertext to where it lives, so a value lifted out of one row
and pasted into another fails to open instead of quietly decrypting. Passing
nil is allowed and means no binding.

The frame header is authenticated too. Without that, rewriting the key ID on a
stored ciphertext would steer decryption at a different key, and the only thing
standing in the way would be that the wrong key happens to fail.

# Errors

Everything that fails to authenticate reports ErrAuthenticationFailed, whether
the cause was tampering, the wrong key, or associated data that does not match.
That is deliberate: distinguishing them for a caller distinguishes them for an
attacker. Bytes that cannot be parsed at all are ErrMalformedCiphertext, which
is a different problem — not a ciphertext this package produced — and a
ciphertext naming a key the ring does not hold is ErrUnknownKeyID, which is
usually an operational problem worth alerting on rather than a security event.
*/
package encryption
