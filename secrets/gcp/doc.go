/*
Package gcp reads secrets from GCP Secret Manager.

A source is bound to one project, named in Config and required, and every
GetSecret is a network round trip to it. Credentials are Application Default
Credentials unless a SecretVersionAccessor is passed in, which is the seam for
tests and for callers that already hold a configured client. Close closes the
client this package opened.

# Always the latest version

A short name is resolved to projects/{project}/secrets/{name}/versions/latest;
a name that already begins with "projects/" is used verbatim, which is the only
way to reach a pinned version or a secret in another project. So by default a
rotation in Secret Manager is picked up by the next lookup, and there is no
Config field that pins a version.

"The next lookup" is doing real work in that sentence. This source is
fetch-per-call and caches nothing, which pushes callers toward resolving
everything at boot and holding the values for the life of the process —
precisely the pattern that turns a rotation into an outage. secrets.NewCachingSource
is the decorator that gives this source a TTL, single-flight, background refresh,
and an OnChange callback; see the secrets package for the argument.

# Errors

A NOT_FOUND from Secret Manager, and a version that comes back with no payload,
are both reported as secrets.ErrSecretNotFound. Everything else — permission
denied, unreachable, quota — passes through as itself. That mapping is the point
of the sentinel: a caller can tell "no such secret" from "could not reach the
provider" without knowing which provider it was handed.

Only the secret's name is put on spans and logs. The value never is.
*/
package gcp
