/*
Package secrets provides a secret retrieval interface with implementations for
environment variables, GCP Secret Manager, AWS SSM Parameter Store, and
Kubernetes secrets.

SecretSource is deliberately narrow — read one secret by name, close what was
opened — so a caller can be handed any provider and a test can be handed a map.

# Caching, refreshing, and rotation

The providers that talk to a network are fetch-per-call, which pushes callers
toward resolving every secret at boot and holding the values for the life of the
process. That is the pattern that turns a key rotation into an outage: the
backend rotates, every running process keeps the old value until someone
redeploys, and nothing in the process can notice.

NewCachingSource is the decorator that removes the reason to do that. It gives
the source a TTL and read-through caching with single-flight, so a stampede of
cold readers costs one backend call; WithRefresh keeps entries warm in the
background so the round-trip leaves the hot path once the cache is warm; and
CachingSource.OnChange reports a value that changed, which is what lets a caller
re-derive whatever it built out of the old one — a signing keyring, a database
credential, an SDK client — without restarting.

Rotation is observed by re-reading on a TTL, not by subscribing. The backends'
change-notification stories are divergent to absent, so polling is the portable
contract; OnChange does not preclude a push-capable provider later, since a push
is only a refresh that arrived early.

secrets/config wires all of this from configuration: set CacheTTL to wrap
whichever provider the config selected, and RefreshInterval to keep it warm.
*/
package secrets
