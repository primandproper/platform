package audithook

import (
	"github.com/primandproper/platform-go/v12/audit"
)

// Option configures the hook Record returns.
//
// It carries no type parameter for the same reason resources.Option does not:
// Go cannot infer a type argument from a call's result type, so an Option[T]
// would have to be spelled out at every option of every call. Nothing here needs
// the row type anyway — an entry is assembled from the change's dimensions, not
// from its columns.
type Option func(*options)

type options struct {
	systemActorID string
	actorType     audit.ActorType
}

// WithSystemActorID names the actor recorded for a write performed by
// resources.System, in place of DefaultSystemActorID.
//
// An application that runs more than one kind of unattended write — a retention
// reaper, a cascade, an importer — names them apart here, one hook per resource
// or one per job, so the log can be asked which of them touched a row.
func WithSystemActorID(id string) Option {
	return func(o *options) {
		if id != "" {
			o.systemActorID = id
		}
	}
}

// WithActorType records a non-system actor as something other than
// audit.ActorUser — a service acting under its own credentials, say.
//
// It is per hook rather than per write because a resource's writes come from one
// kind of principal: the resource whose rows users write is not also the one a
// service writes. A resource that genuinely takes both wants two stores, or a
// hook of its own.
func WithActorType(actorType audit.ActorType) Option {
	return func(o *options) {
		if actorType != "" {
			o.actorType = actorType
		}
	}
}
