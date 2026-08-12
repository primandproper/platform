/*
Package aws wraps key material with AWS KMS.

# What choosing it commits you to

The wrapping key never enters this process: KMS performs the wrap and unwrap
inside its own boundary and hands back only the result, which is the whole reason
to prefer this over the local wrapper. Key policy, rotation, and the audit trail
belong to KMS rather than to the application, and revoking the application's
access to the key revokes its ability to open everything that key wrapped.

It is also why every Wrap and Unwrap is a network round trip, billed and
rate-limited per key. A per-subject data key should be unwrapped once and cached
for as long as it is needed, not unwrapped per row read; envelope encryption
stops paying for itself the moment the unwrap is on the hot path.

Absent a client, one is built from the default credential chain, so the
deployment's usual role and region resolution apply.

# Associated data is not a structured encryption context

AWS models associated data as an encryption context — a map of printable
strings — where every other AEAD in this module takes an opaque byte string.
Rather than push that difference up into the KeyWrapper interface, the bytes are
base64'd into a single fixed entry. It round-trips exactly, and the cost is that
CloudTrail shows a wrap happened with some binding rather than showing what the
binding was.

Unwrapping requires the identical associated data. It is authenticated, not
stored: get it wrong and the unwrap fails rather than returning different bytes.
*/
package aws
