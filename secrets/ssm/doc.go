/*
Package ssm reads secrets from AWS SSM Parameter Store.

A source is bound to one region, which Config requires, and every GetSecret is a
network round trip. Credentials come from the default AWS credential chain —
instance role, container role, environment, shared profile — unless a
GetParameterAPI is passed in, which is the seam for tests and for callers that
already hold a configured client. Choosing this source therefore commits a
deployment to whatever IAM identity the process runs as.

# Decryption is always requested

Every lookup sets WithDecryption, so a SecureString parameter comes back in
plaintext. That is what makes this source usable for secrets rather than for
configuration, and it means the calling identity needs whatever KMS permission
the parameter's key requires, not only ssm:GetParameter. A parameter store used
only for plain strings is unaffected.

# Names and the prefix

Parameter names are '/'-delimited hierarchies. A name starting with "/" is
absolute and used as given. Otherwise Config.Prefix is joined to it with exactly
one separator, so Prefix "/app" and name "db_password" resolve to
"/app/db_password" rather than "/appdb_password". An empty prefix leaves the
name alone.

# Caching

This source is fetch-per-call and caches nothing, which pushes callers toward
resolving everything at boot and holding the values for the life of the process
— precisely the pattern that turns a rotation into an outage.
secrets.NewCachingSource is the decorator that gives it a TTL, single-flight,
background refresh, and an OnChange callback; see the secrets package for the
argument.

# Errors

A ParameterNotFound, and a response with no parameter, are both reported as
secrets.ErrSecretNotFound. Everything else — access denied, throttling,
unreachable — passes through as itself, so a caller can tell "no such secret"
from "could not reach the provider" without knowing which provider it was
handed.

Close is a no-op: the SSM client is a stateless HTTP client. Only the resolved
parameter name is put on spans and logs, never the value.
*/
package ssm
