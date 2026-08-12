/*
Package env reads secrets from this process's environment.

It is the secret source with nothing behind it: no network, no credentials, no
vendor, no client to close. Config is empty and NewSecretSource takes no
required argument. GetSecret is os.LookupEnv with the observability every other
source records — the lookup key on the span, never the value.

# What that buys, and what it costs

There is no failure mode between the caller and the value. A lookup cannot time
out, be throttled, or fail to authenticate, and there is no per-call cost worth
caching: wrapping this source in secrets.NewCachingSource adds a TTL and a
refresh loop over an in-memory map read, which is work for nothing.

The cost is that rotation is invisible here. A process's environment is fixed at
exec, so a secret rotated in the backing store does not reach a running process
and no TTL can discover that it changed — the value this source returns on its
ten-thousandth call is the value the process started with. Where key rotation
must be observed without a redeploy, the gcp or ssm source under
secrets.NewCachingSource is the arrangement that does it; see the secrets
package's own documentation.

A variable that is not set returns secrets.ErrSecretNotFound, so a missing
secret stays distinguishable from one whose value is legitimately empty.

Close logs and returns nil. There is nothing to release.
*/
package env
