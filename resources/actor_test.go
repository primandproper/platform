package resources_test

import (
	"testing"

	"github.com/primandproper/platform-go/v12/resources"

	"github.com/shoenig/test"
)

func TestActor(T *testing.T) {
	T.Parallel()

	T.Run("the zero actor names nobody", func(t *testing.T) {
		t.Parallel()

		var actor resources.Actor

		test.ErrorIs(t, actor.Validate(), resources.ErrNoActor)
		test.False(t, actor.IsSystem())
		test.EqOp(t, "", actor.ID())
		test.EqOp(t, "<unset>", actor.String())
	})

	T.Run("an empty id is the zero actor rather than an anonymous one", func(t *testing.T) {
		t.Parallel()

		// A caller reaching ActingAs with an empty string has had a session
		// lookup come back empty, and that should surface here rather than as a
		// write that quietly matched nothing.
		test.ErrorIs(t, resources.ActingAs("").Validate(), resources.ErrNoActor)
	})

	T.Run("a named actor carries its id", func(t *testing.T) {
		t.Parallel()

		actor := resources.ActingAs("user_alice")

		test.NoError(t, actor.Validate())
		test.False(t, actor.IsSystem())
		test.EqOp(t, "user_alice", actor.ID())
		test.EqOp(t, "user_alice", actor.String())
	})

	T.Run("the system actor is not the zero one", func(t *testing.T) {
		t.Parallel()

		actor := resources.System()

		test.NoError(t, actor.Validate())
		test.True(t, actor.IsSystem())
		test.EqOp(t, "", actor.ID())
		test.EqOp(t, "<system>", actor.String())
	})

	T.Run("an id of \"system\" stays distinguishable from the system actor", func(t *testing.T) {
		t.Parallel()

		test.NotEq(t, resources.System().String(), resources.ActingAs("system").String())
		test.False(t, resources.ActingAs("system").IsSystem())
	})
}
