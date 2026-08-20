package audithook_test

import (
	"context"
	"testing"

	"github.com/primandproper/platform-go/v12/audit"
	"github.com/primandproper/platform-go/v12/database"
	platformerrors "github.com/primandproper/platform-go/v12/errors"
	"github.com/primandproper/platform-go/v12/resources"
	"github.com/primandproper/platform-go/v12/resources/audithook"
	"github.com/primandproper/platform-go/v12/tenancy"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// comment is a row type; nothing here reads its fields, because an entry is
// assembled from the change's dimensions rather than from its columns.
type comment struct {
	ID string
}

// capturing is a Recorder that keeps what it was handed.
type capturing struct {
	err     error
	entries []*audit.Entry
}

var _ audit.Recorder = (*capturing)(nil)

func (c *capturing) Record(_ context.Context, _ database.SQLQueryExecutor, entries ...*audit.Entry) error {
	if c.err != nil {
		return c.err
	}

	c.entries = append(c.entries, entries...)

	return nil
}

// change is one write for the hook to translate.
func change(op resources.Op, actor resources.Actor) resources.Change[comment] {
	return resources.Change[comment]{
		Op:       op,
		Resource: "comment",
		Table:    "comments",
		ID:       "comment_1",
		Owner:    "user_alice",
		Scope:    tenancy.Of("account_1"),
		Actor:    actor,
	}
}

func TestRecord(T *testing.T) {
	T.Parallel()

	T.Run("translates a write into an entry", func(t *testing.T) {
		t.Parallel()

		recorder := &capturing{}
		hook := audithook.Record[comment](recorder)

		must.NoError(t, hook(t.Context(), nil, change(resources.OpUpdated, resources.ActingAs("user_alice"))))

		must.SliceLen(t, 1, recorder.entries)

		entry := recorder.entries[0]
		test.EqOp(t, audit.EventUpdated, entry.EventType)
		test.EqOp(t, "comment", entry.ResourceType)
		test.EqOp(t, "comment_1", entry.ResourceID)
		test.EqOp(t, "user_alice", entry.Actor.ID)
		test.EqOp(t, audit.ActorUser, entry.Actor.Type)
		// The scope is the chain partition and a column value, so it is the
		// identifier rather than the prose tenancy.Scope.String renders.
		test.EqOp(t, "account_1", entry.Scope)
		test.EqOp(t, "comments", entry.Metadata["table"])
	})

	T.Run("maps each write to the event that describes it", func(t *testing.T) {
		t.Parallel()

		for op, expected := range map[resources.Op]audit.EventType{
			resources.OpCreated:  audit.EventCreated,
			resources.OpUpdated:  audit.EventUpdated,
			resources.OpArchived: audit.EventArchived,
			resources.Op("what"): audit.EventOther,
		} {
			recorder := &capturing{}

			must.NoError(t, audithook.Record[comment](recorder)(t.Context(), nil, change(op, resources.ActingAs("user_alice"))))
			must.SliceLen(t, 1, recorder.entries)
			test.EqOp(t, expected, recorder.entries[0].EventType, test.Sprintf("op %q", op))
		}
	})

	T.Run("the system actor is named rather than empty", func(t *testing.T) {
		t.Parallel()

		recorder := &capturing{}

		// audit refuses an entry with no actor id, so the system actor needs a
		// name — and "the platform did this" is a different fact from "nobody
		// said who did this".
		must.NoError(t, audithook.Record[comment](recorder)(t.Context(), nil, change(resources.OpArchived, resources.System())))

		must.SliceLen(t, 1, recorder.entries)
		test.EqOp(t, audithook.DefaultSystemActorID, recorder.entries[0].Actor.ID)
		test.EqOp(t, audit.ActorSystem, recorder.entries[0].Actor.Type)
	})

	T.Run("the system actor's name and the actor type are the caller's to choose", func(t *testing.T) {
		t.Parallel()

		recorder := &capturing{}
		hook := audithook.Record[comment](recorder,
			audithook.WithSystemActorID("retention_reaper"),
			audithook.WithActorType(audit.ActorService),
		)

		must.NoError(t, hook(t.Context(), nil, change(resources.OpArchived, resources.System())))
		must.NoError(t, hook(t.Context(), nil, change(resources.OpUpdated, resources.ActingAs("importer"))))

		must.SliceLen(t, 2, recorder.entries)
		test.EqOp(t, "retention_reaper", recorder.entries[0].Actor.ID)
		test.EqOp(t, audit.ActorService, recorder.entries[1].Actor.Type)
	})

	T.Run("an entry that cannot be appended fails the write", func(t *testing.T) {
		t.Parallel()

		boom := platformerrors.New("boom")
		hook := audithook.Record[comment](&capturing{err: boom})

		// The hook runs inside the write's transaction, so returning the
		// recorder's error is what keeps the row and the record of it from
		// disagreeing.
		test.ErrorIs(t, hook(t.Context(), nil, change(resources.OpCreated, resources.ActingAs("user_alice"))), boom)
	})

	T.Run("a nil recorder is refused rather than treated as no auditing", func(t *testing.T) {
		t.Parallel()

		hook := audithook.Record[comment](nil)

		test.ErrorIs(t,
			hook(t.Context(), nil, change(resources.OpCreated, resources.ActingAs("user_alice"))),
			platformerrors.ErrNilInputParameter,
		)
	})
}
