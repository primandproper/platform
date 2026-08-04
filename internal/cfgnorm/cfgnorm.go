// Package cfgnorm holds the normalization a config performs on itself before
// its own validation runs.
//
// It exists because of one interaction. Pointer sub-config fields carry
// `env:",init"` so that a deployment can set FILESYSTEM_ROOT_DIRECTORY without
// also having to supply a `filesystem` block in the JSON — without it, env
// parsing skips a nil pointer and the variable is silently never read. The
// consequence is that after env parsing every provider's sub-config is
// allocated, whether or not that provider was selected.
//
// That makes a pointer's nil-ness meaningless: it records whether env parsing
// ran, not whether the operator configured anything. Rules written as
// "the sub-configs for providers you did not select must be Nil" therefore
// reject every config that has been through env.Parse, for every provider.
//
// ZeroToNil restores the intended meaning by releasing the pointers that were
// allocated and never filled in, so a non-nil sub-config means the operator put
// something in it — which is what those rules were written to check.
//
// # Which of the two fixes a seam needs
//
// The allocation breaks validation in two distinct ways, and they take different
// remedies. Reach for ZeroToNil only for the first.
//
//  1. A rule reads the pointer's nil-ness — validation.Nil on the sub-configs
//     for unselected providers, or a Required that a non-nil pointer to a zero
//     struct silently satisfies. Normalizing is the fix: the rule is about
//     presence, and ZeroToNil is what makes presence mean what it says.
//     uploads/objectstorage, secrets, authorization and the observability
//     pillars are this shape.
//
//  2. Nothing reads nil-ness, but the sub-config is validated anyway. ozzo
//     validates any non-nil pointer to a Validatable once a field's rules have
//     run, whatever those rules were, so a validation.When(selected, Required)
//     guard stops the Required rule and nothing else — the unselected provider's
//     own Required rules are still enforced. The fix is
//     validation.Skip.When(not selected) as the field's first rule, which is
//     ozzo's own way to say "do not look at this at all". cache, email, llm and
//     the other provider seams are this shape.
//
// ZeroToNil is not a substitute for the second case, and not only because the
// rules do not need it: a sub-config with envDefault fields is never zero once
// the environment has been parsed, so there is nothing for it to release.
// search/vector and distributedlock are where that bites.
//
// The cost of skipping is that a sub-config the operator filled in for a
// provider they did not select goes unchecked. That is already what those seams
// mean — none of them make configuring an unselected provider an error — and it
// is the same trade the Nil rules make in the other direction.
package cfgnorm

import "reflect"

// ZeroToNil sets *p to nil when it points at the zero value of T.
//
// Call it at the top of ValidateWithContext, before the rules that ask whether
// a sub-config is present. It is deliberately not a defaults step: it removes
// information that was never there rather than supplying any.
//
// A sub-config the operator did fill in is left alone, so selecting one provider
// and configuring another is still the error it should be.
func ZeroToNil[T any](p **T) {
	if p == nil || *p == nil {
		return
	}

	if reflect.ValueOf(*p).Elem().IsZero() {
		*p = nil
	}
}
