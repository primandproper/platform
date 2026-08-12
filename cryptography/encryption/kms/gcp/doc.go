/*
Package gcp wraps key material with Google Cloud KMS.

# What choosing it commits you to

The wrapping key never enters this process: Cloud KMS performs the wrap and
unwrap inside its own boundary and hands back only the result, which is the whole
reason to prefer this over the local wrapper. Key rotation, IAM, and the audit
trail belong to Cloud KMS rather than to the application, and revoking the
application's access revokes its ability to open everything that key wrapped.

It is also why every Wrap and Unwrap is a network round trip, billed and quota'd
per key. A per-subject data key should be unwrapped once and cached for as long
as it is needed, not unwrapped per row read; envelope encryption stops paying for
itself the moment the unwrap is on the hot path.

Associated data goes to the API as-is, as the request's additional authenticated
data, so no encoding is imposed on it — unlike the aws sibling, which has to
encode it to fit AWS's string-map encryption context. It is authenticated rather
than stored: an unwrap with different associated data fails rather than returning
different bytes.

# Closing matters here

Absent a client, this package builds one from Application Default Credentials and
therefore owns a gRPC connection. Close releases it. That method is on the
concrete *KeyWrapper and not on encryption.KeyWrapper, which is a reason to hold
what NewKeyWrapper returned rather than narrowing it to the interface at the
point of construction.

A failed unwrap is reported as it arrived. Cloud KMS answers a tampered
ciphertext, mismatched associated data, a destroyed key version, and a revoked
permission with the same code, and collapsing that into an authentication failure
would report an operational problem as an attack.
*/
package gcp
