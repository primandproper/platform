package cfgnorm

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

type withDefaults struct {
	Name string
	Size int
}

func (d *withDefaults) EnsureDefaults() {
	if d.Size == 0 {
		d.Size = 42
	}
}

type parent struct {
	Defaulted *withDefaults
	Plain     *sub
	Value     withDefaults
}

func TestEnsureSubDefaults(T *testing.T) {
	T.Parallel()

	T.Run("defaults a present sub-config", func(t *testing.T) {
		t.Parallel()

		p := &parent{Defaulted: &withDefaults{Name: "configured"}}
		must.NoError(t, EnsureSubDefaults(p))

		test.EqOp(t, 42, p.Defaulted.Size)
	})

	T.Run("leaves what the operator set alone", func(t *testing.T) {
		t.Parallel()

		p := &parent{Defaulted: &withDefaults{Size: 1}}
		must.NoError(t, EnsureSubDefaults(p))

		test.EqOp(t, 1, p.Defaulted.Size)
	})

	T.Run("has nothing to default on an absent sub-config", func(t *testing.T) {
		t.Parallel()

		// The whole reason this runs after UnconfiguredToNil rather than
		// before: a released sub-config has to stay released.
		p := &parent{}
		must.NoError(t, EnsureSubDefaults(p))

		test.Nil(t, p.Defaulted)
	})

	T.Run("skips a sub-config that has no defaults of its own", func(t *testing.T) {
		t.Parallel()

		p := &parent{Plain: &sub{}}
		must.NoError(t, EnsureSubDefaults(p))

		test.NotNil(t, p.Plain)
	})

	T.Run("considers only the pointer fields", func(t *testing.T) {
		t.Parallel()

		// A value field is not a presence decision, so it is not this
		// function's business — whoever owns it defaults it.
		p := &parent{}
		must.NoError(t, EnsureSubDefaults(p))

		test.EqOp(t, 0, p.Value.Size)
	})

	T.Run("rejects what it cannot walk", func(t *testing.T) {
		t.Parallel()

		must.Error(t, EnsureSubDefaults(parent{}))
		must.Error(t, EnsureSubDefaults((*parent)(nil)))
		must.Error(t, EnsureSubDefaults(nil))
	})
}
