/*
Package posthog evaluates feature flags against PostHog, by way of OpenFeature.

Choosing it commits a deployment to two PostHog credentials, not one. The
project API key identifies the project; the personal API key is what the SDK
requires before it will evaluate a flag at all, and a config carrying only the
project key is refused at validation rather than building an object that fails
every evaluation. Endpoint selects EU Cloud or a self-hosted instance; empty
means PostHog US Cloud.

A personal API key is a user-scoped credential with broad access, which is a
different thing to hand a deployment than a project key. That is part of the
cost of this provider, and it is the reason the launchdarkly sibling needs only
one secret.

# A missing boolean flag is indistinguishable from a disabled one

PostHog's API answers false for a boolean flag it has never heard of, and there
is nothing in that answer to separate it from a flag that exists and is off. So
CanUseFeature returns (false, nil) for a flag nobody has created — it never
reports featureflags.ErrFlagNotFound. The four typed getters do, because a
string, number, or object flag has a value to be missing.

Callers relying on the three-way distinction featureflags documents should read
that as: with this provider, the boolean path collapses two of the three answers
into the inert one. It is the same outcome by a shorter route, not an error being
swallowed.

# Evaluation

The five evaluation methods and their circuit-breaker protocol come from
featureflags/internal/openfeatureflags, embedded, and are the same code the
launchdarkly sibling runs. WithConfigModifiers reaches posthog.Config before the
client is built, for anything this package's Config does not name.

# Close is mandatory

Each construction registers a uniquely-named provider in OpenFeature's
process-global registry, and that registry has no removal API. Close detaches
the registration by replacing it with the no-op provider and then closes the
PostHog client. Skipping it leaks the client — and a service that rebuilds its
flag manager on config reload leaks one per cycle until the process exits.
*/
package posthog
