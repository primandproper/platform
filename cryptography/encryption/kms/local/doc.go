/*
Package local wraps key material with an encryption.Cipher held in this process.

# What choosing it commits you to

It is the encryption.KeyWrapper for deployments with no KMS: the wrapping key
comes from wherever the cipher was built — an environment variable, a mounted
secret — and lives in this process's memory for as long as the process does. That
puts it in a heap dump, in a core file, and in reach of anything that can read
the process, which is exactly the exposure the aws and gcp siblings exist to
remove. Those perform the wrap inside the KMS boundary and the wrapping key never
enters the application at all.

It is still not nothing. An attacker holding the database and not the process
gets wrapped keys they cannot open, which is the whole point of wrapping; what
this implementation does not survive is compromise of the application itself.

There are no network calls, so unlike the cloud wrappers a wrap or unwrap costs
nothing and cannot fail from unavailability, rate limiting, or credentials.

Rotating the wrapping key means re-wrapping every stored key, and nothing here
does that. What a wrapped key can be opened with is whatever the supplied Cipher
decides — the aes cipher's output is nonce, ciphertext, and tag with no key
identifier in front of it, so under that cipher a changed key makes every
existing wrapped key unopenable rather than merely stale.

Nothing on this path is logged or spanned with lengths or contents. Everything
passing through is key material, and the observability that is routine one layer
up is a leak at this one.
*/
package local
