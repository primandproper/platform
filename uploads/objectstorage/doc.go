/*
Package objectstorage is the uploads.UploadManager backed by gocloud.dev/blob.

One package covers six backends, selected by Config.Provider: "s3", "gcp", "r2",
"backblaze_b2", "filesystem", and "memory". An unrecognized value is
errors.ErrUnknownProvider rather than a working-looking default, and the
sub-config for a provider must be present when that provider is named and absent
otherwise, so a config carrying R2 credentials while naming S3 is refused rather
than quietly ignored.

# What each choice commits a deployment to

  - s3 and gcp take no credentials from Config. They resolve the ambient chain —
    the AWS default chain, GCP Application Default Credentials — so they commit
    the deployment to whatever identity the process runs as, and rotate
    underneath it without a rebuild.
  - r2 and backblaze_b2 take static keys from Config and construct the vendor
    endpoint from an account ID or region. Both speak S3's API; what they cost
    is a long-lived key pair this module has to be handed.
  - filesystem writes under a root directory, creating directories at 0700
    rather than gocloud's 0777 default so other users on the host cannot
    traverse in. DirectoryMode overrides that and parses as octal, because every
    way anyone writes a Unix mode is octal.
  - memory keeps objects in this process. Nothing survives the process and
    nothing is shared between replicas; it is for tests and local runs.

# Capabilities are implemented, not therefore supported

Uploader satisfies every optional interface in uploads — RangeReader, URLSigner,
Attributer, Lister — because gocloud exposes all four uniformly. Whether a given
backend can honor them is a separate question, and the one that bites is
signing: SignedURL fails on memory, and on filesystem, which is opened with no
URL signer. A caller that needs signed URLs across environments needs to know
that the development backend cannot mint them.

# Construction does not touch the network

No provider is probed for reachability at construction, GCP included. What
gocloud offers is a list operation, and listing is a distinct permission from
reading and writing — the least-privilege policy for a service that only saves
and opens grants neither, so a probe would refuse a bucket the Uploader can use
perfectly well, and refuse it at startup where the deployment cannot proceed. It
would also make a transient blip during a rollout into a service that never
comes up.

Unreachability is modeled at runtime instead: every operation runs through a
circuit breaker built from Config.CircuitBreaker, and a rejected one returns
circuitbreaking.ErrCircuitBroken naming the operation, with the provider's own
error behind it on the way in.

# BucketPrefix

When set, the prefix gets a trailing "/" whether or not the config supplied one.
gocloud concatenates the prefix with the key verbatim, so a prefix of "acme"
would turn key "1/x" into "acme1/x" — which is also what tenant "acme1" writes,
silently sharing a namespace and returning each other's objects from List.

# Writes are all-or-nothing

gocloud commits a write when the writer closes without error, so Save cancels
the write's context before closing when the copy fails. Without that, a
truncated object would be committed at the path while Save returned an error.
*/
package objectstorage
