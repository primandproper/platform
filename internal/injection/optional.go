// Package injection holds the samber/do helpers shared by this module's
// do.Provide registrations.
//
// It exists because the observability package cannot be the single home for
// them: it imports the four pillar config subpackages in order to build a
// Pillars, so those subpackages cannot import it back. Both sides reach the
// same helper through here instead.
package injection

import (
	stderrors "errors"

	"github.com/samber/do/v2"
)

// InvokeOptional resolves a service that need not be registered.
//
// It distinguishes the two failures a plain do.Invoke conflates: "nobody
// registered one", which yields the zero value and no error, and "the
// registered one failed to build", which is returned. That distinction is the
// whole point — a dependency nobody wanted should not stop a container from
// wiring up, and a dependency somebody configured wrongly should not be
// silently swapped for its absence.
func InvokeOptional[T any](i do.Injector) (T, error) {
	svc, err := do.Invoke[T](i)
	if err != nil && stderrors.Is(err, do.ErrServiceNotFound) {
		var zero T

		return zero, nil
	}

	return svc, err
}
