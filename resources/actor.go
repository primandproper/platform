package resources

import (
	platformerrors "github.com/primandproper/platform-go/v12/errors"
)

// ErrNoActor indicates a write that named nobody — the zero Actor reaching a
// method on a resource whose rows have an owner.
//
// It is not a permission failure. ErrPermissionDenied is what a write by the
// wrong actor produces; this one means the call never said who was writing.
var ErrNoActor = platformerrors.Wrap(platformerrors.ErrEmptyInputParameter, "no actor provided")

// Actor names who is performing a write.
//
// It is a struct with unexported fields for the same reason tenancy.Scope is:
// the zero value names nobody and cannot be mistaken for someone. A write
// against an owned resource by an unset Actor fails with ErrNoActor rather than
// matching whatever the owner column happens to hold.
//
// Build one with ActingAs or System.
type Actor struct {
	// id is the owner identifier a write is checked against.
	id string
	// system separates a deliberate absence of a user — a background job — from
	// the zero value, which is the absence of a decision.
	system bool
	// known is set by both constructors, so that the zero value is
	// distinguishable from either.
	known bool
}

// ActingAs returns the actor identified by id.
//
// An empty id yields the zero Actor, which no owned write accepts. That is
// deliberate: ActingAs takes an identifier the caller is holding, and an empty
// one means their own session lookup came back empty — which should surface as
// ErrNoActor rather than as a write that quietly matched nothing.
func ActingAs(id string) Actor {
	if id == "" {
		return Actor{}
	}

	return Actor{id: id, known: true}
}

// System returns the actor for a write with no user behind it: a cascade, a
// retention reaper, a scheduled finalizer.
//
// It satisfies the owner requirement without narrowing the write to one owner,
// which is what lets a cascade archive every author's comments on a deleted
// recipe. That is a real widening of what a write can touch, so it is a named
// constructor rather than something an empty string falls into.
func System() Actor {
	return Actor{system: true, known: true}
}

// Validate reports whether the actor names anything, returning ErrNoActor when
// it does not.
func (a Actor) Validate() error {
	if !a.known {
		return ErrNoActor
	}

	return nil
}

// IsSystem reports whether this is the actor with no user behind it. The zero
// Actor is not the system actor — it is undecided.
func (a Actor) IsSystem() bool {
	return a.known && a.system
}

// ID returns the owner identifier this actor names, or the empty string for the
// system actor. The zero Actor also returns the empty string, so ID alone
// cannot tell them apart; call Validate first.
func (a Actor) ID() string {
	return a.id
}

// String renders the actor for a log field or a span attribute. The angle
// brackets are there so an id that happens to be "system" stays distinguishable.
func (a Actor) String() string {
	switch {
	case !a.known:
		return "<unset>"
	case a.system:
		return "<system>"
	default:
		return a.id
	}
}
