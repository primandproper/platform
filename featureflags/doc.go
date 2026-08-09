/*
Package featureflags provides a feature flag evaluation interface for controlling
feature availability per user, with implementations for LaunchDarkly and PostHog.

# Three answers, not two

An evaluation can end three ways, and the difference between the last two is the
reason this package has a sentinel of its own:

	(value, nil)               the provider decided
	(default, ErrFlagNotFound) the provider answered, and no such flag exists
	(default, err)             the provider could not answer

The middle case used to be indistinguishable from the last, which made it a bug
rather than a distinction. Every provider here guards its evaluations with a
circuit breaker, and that breaker is one per FeatureFlagManager, shared by every
flag the process evaluates. Reporting a missing flag as a provider failure
therefore made evaluating a flag nobody had created a vote to disable every other
flag in the process.

That is not a hypothetical shape. A rollout normally ships the code that reads the
flag before the flag itself is created, so for the length of that window the flag
name is live in code and absent from the provider. Under real request volume that
is enough to open the breaker, at which point flags that do exist and are
load-bearing start returning circuitbreaking.ErrCircuitBroken.

So a FLAG_NOT_FOUND resolution returns ErrFlagNotFound and scores the breaker a
success. The breaker exists to give a failing service breathing room, and
answering "no such flag" is not what a failing service does — it is a correct
negative answer, which is health. Everything else — an unready provider, an
unreachable one, a flag whose value will not parse — is still a failure the
breaker hears about.

Drawing the line at all takes a backend that can tell a missing flag from a
present one, and that is not universal. PostHog's API answers false for a boolean
flag it has never heard of, indistinguishably from one that exists and is off, so
its CanUseFeature returns (false, nil) and never reports the flag missing. The
outcome is the same inert answer by a shorter route. What this package guarantees
is not that an unresolvable flag surfaces as an error — it is that when one does,
it says which kind it is.

# Choosing what a missing flag means

The value returned alongside ErrFlagNotFound is the caller's default, so a caller
with nothing better to do can ignore which error it got and take the value. A
caller that does care matches the sentinel:

	enabled, err := flags.CanUseFeature(ctx, "new-checkout", evalCtx)
	switch {
	case errors.Is(err, featureflags.ErrFlagNotFound):
		// Not created yet. Not the same as off, and not an outage.
	case err != nil:
		// The provider could not answer.
	default:
		// enabled is a decision.
	}

CanUseFeature deliberately takes no default parameter, unlike its four siblings.
Their defaults are real values in a range; a boolean default has two settings and
one of them is degenerate, because "an unresolvable flag should not stop me" is
the same as not consulting the flag on that path at all.
*/
package featureflags
