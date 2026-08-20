// Package audithook writes a resources Store's writes into the audit log, in
// the transaction that performed them.
//
// A resources.Hook takes the executor its write is running under, and
// audit.Recorder.Record takes an executor to append through. The two seams are
// the same seam, which is what makes this adapter a translation rather than a
// mechanism: a resource declared with this hook records every create, update and
// archive it performs, and a write whose entry cannot be appended is a write
// that does not commit.
//
// It is a separate package rather than an option on resources so that a resource
// which audits nothing does not import an audit recorder. That is the same
// reason resources has hooks at all.
//
//	store, err := resources.NewStore(comments, client,
//		resources.WithHook(audithook.Record[Comment](recorder)),
//	)
//
// # What it records, and what it does not
//
// An entry names what happened, to which resource, by whom, in what scope. It
// carries no field-level diff, and that omission is deliberate: a diff needs the
// row as it was before the write, which the store would have to read on every
// update whether or not anybody was auditing. The caller that is about to call
// Update already holds that image — it fetched the row in order to modify it —
// so a consumer who wants a diff writes a hook that closes over its own before
// image and calls audit.Diff, which is a handful of lines at the one place the
// before image exists.
package audithook

import (
	"context"

	"github.com/primandproper/platform-go/v12/audit"
	"github.com/primandproper/platform-go/v12/database"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/resources"
)

// DefaultSystemActorID is the actor id recorded for a write performed by
// resources.System.
//
// An entry needs an actor — audit refuses one without an id — and the system
// actor has no user behind it, so it needs a name rather than an empty string.
// It is a constant rather than an empty default because "the platform did this"
// and "nobody said who did this" are different facts about an audit entry, and a
// log that spells the first as the second cannot be asked which writes were the
// application's own.
const DefaultSystemActorID = "system"

// tableKey is the metadata key the row's table lands under. The entry already
// names the resource; the table is what a reader needs to go and look at the row.
const tableKey = "table"

// Record returns a resources.Hook that appends one audit entry per write.
//
// A nil recorder is refused at the write rather than at construction, and
// refused rather than ignored: a hook that records nothing is what not calling
// WithHook already produces, and quietly being one here would mean a resource
// declared as audited was not.
func Record[T any](recorder audit.Recorder, opts ...Option) resources.Hook[T] {
	settings := &options{
		systemActorID: DefaultSystemActorID,
		actorType:     audit.ActorUser,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(settings)
		}
	}

	return func(ctx context.Context, exec database.SQLQueryExecutor, change resources.Change[T]) error {
		if recorder == nil {
			return platformerrors.Wrap(platformerrors.ErrNilInputParameter, "audithook: no audit recorder")
		}

		return recorder.Record(ctx, exec, &audit.Entry{
			EventType:    eventFor(change.Op),
			ResourceType: change.Resource,
			ResourceID:   change.ID,
			Scope:        change.Scope.Owner(),
			Actor:        settings.actor(change.Actor),
			Metadata:     map[string]string{tableKey: change.Table},
		})
	}
}

// actor translates a resources.Actor into the audit log's.
//
// The scope beside it is the tenancy scope's Owner rather than its String: the
// entry's scope is the chain partition and a column value, and "<global>" is
// prose. See tenancy.Scope.Owner.
func (o *options) actor(actor resources.Actor) audit.Actor {
	if actor.IsSystem() {
		return audit.Actor{ID: o.systemActorID, Type: audit.ActorSystem}
	}

	return audit.Actor{ID: actor.ID(), Type: o.actorType}
}

// eventFor maps a write to the audit event that describes it.
//
// The two vocabularies are close but not identical, and the mapping is here
// rather than in either package because neither should own the other's words:
// audit's set is open — a consumer whose domain distinguishes "approved" from
// "updated" says so — and resources' set is closed, being the three things a
// store can do to a row.
func eventFor(op resources.Op) audit.EventType {
	switch op {
	case resources.OpCreated:
		return audit.EventCreated
	case resources.OpUpdated:
		return audit.EventUpdated
	case resources.OpArchived:
		return audit.EventArchived
	default:
		return audit.EventOther
	}
}
