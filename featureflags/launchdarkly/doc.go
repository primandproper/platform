/*
Package launchdarkly evaluates feature flags against LaunchDarkly, by way of
OpenFeature.

Choosing it commits a deployment to a LaunchDarkly SDK key and to an
*http.Client, which is required rather than optional: the SDK is configured with
it, so a nil one is a construction error rather than a default transport.

# Construction is a network call

MakeCustomClient blocks until LaunchDarkly has delivered the initial flag
payload or InitTimeout — five seconds when unset — elapses. A service that
cannot reach LaunchDarkly at startup therefore fails to build its flag manager
rather than coming up and answering every flag with its default, which is
indistinguishable from the flags being off. The corollary is that this
constructor is one of the few in the module that can be slow, and the timeout is
the knob for how slow.

WithConfigModifiers reaches ld.Config before the client is built, which is where
a caller substitutes the data source — a file-backed or offline one, for
instance — for a test or an air-gapped environment.

# Evaluation

The five evaluation methods and their circuit-breaker protocol come from
featureflags/internal/openfeatureflags, embedded, and are the same code the
posthog sibling runs. LaunchDarkly does distinguish a flag it has never heard of
from one that is off, so the four typed getters report featureflags.ErrFlagNotFound
for a missing key and that outcome scores the breaker a success rather than a
failure. CanUseFeature never reports it, for the reason its documentation there
gives.

# Close is mandatory

Each construction registers a uniquely-named provider in OpenFeature's
process-global registry, and that registry has no removal API. Close detaches
the registration by replacing it with the no-op provider and then closes the
LaunchDarkly client. Skipping it leaks the client — and a service that rebuilds
its flag manager on config reload leaks one per cycle, each still holding a
LaunchDarkly connection, until the process exits.
*/
package launchdarkly
