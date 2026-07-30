/*
Package hmac provides keyed hashing.Hasher implementations, for the cases where
a digest has to prove who computed it rather than only what was computed.

The key is bound at construction rather than passed to Hash, which is what lets
an HMAC satisfy the plain hashing.Hasher interface — and therefore travel
anywhere a Hasher is already accepted, including hashing.Hex. A keyed hasher is
one value per key, so the caller holds a hasher per signing key instead of
threading the key through every call site.

Verification compares through Equal, never through == on a hex rendering. Both
work; only one of them is constant-time, and the difference is a practical
forgery oracle when the candidate MAC is attacker-supplied.
*/
package hmac
