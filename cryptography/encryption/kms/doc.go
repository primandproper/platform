/*
Package kms groups the encryption.KeyWrapper implementations.

A KeyWrapper encrypts key material rather than data, which is what makes
envelope encryption possible: generate a data key per row, per subject, or per
tenant, wrap it, store the wrapped bytes next to what it protects, and keep
exactly one key in the KMS regardless of how many data keys exist.

The point of doing it this way is cost and blast radius at the same time. A
cloud KMS charges per key and rate-limits per key, so a key per subject is
neither affordable nor fast at any real scale — but a key per subject is
exactly what crypto-shredding needs, because destroying it is what makes the
data it protected unrecoverable everywhere at once, including in backups that
have already shipped. Wrapping resolves that: the per-subject key is a row you
can delete, and the key that protects it never leaves the KMS.

The wrapped key is therefore the only copy. Deleting it is not a soft delete
and nothing else can reconstruct it — which is the feature, and also the way
this goes very wrong if a keys table is backed up on the same schedule as the
data it protects. A restore that resurrects a wrapped key resurrects
everything it opened.

# Local wrapping

The local subpackage wraps with a key held in this process, and is the
implementation to use behind an environment variable or a Kubernetes secret
where nothing better exists. It is a weaker thing wearing the same interface:
the wrapping key is in memory, so it is in a core dump, and an attacker who
reaches the process reaches it. It is honest for development and for
deployments that have no KMS, and it should not be mistaken for the guarantee
the cloud implementations provide.
*/
package kms
