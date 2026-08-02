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
