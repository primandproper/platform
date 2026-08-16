package fake

import (
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// node reaches itself directly, which is the cheapest shape that can tell one
// recursion bound from another: the length of the chain hanging off the root is
// the bound the builder applied.
type node struct {
	Child *node
	Name  string
}

// left and right reach themselves the other way, around a cycle. faker counts
// per type rather than per level, so a bound that only stopped a field naming
// its own struct would populate this pair forever.
type left struct {
	Right *right
	Name  string
}

type right struct {
	Left *left
	Name string
}

// chainLength counts the nodes hanging below the root, so a root whose Child is
// nil measures 0.
func chainLength(n *node) int {
	depth := 0
	for n = n.Child; n != nil; n = n.Child {
		depth++
	}

	return depth
}

func TestBuildersShareOneRecursionBound(T *testing.T) {
	T.Parallel()

	// The bug this covers is not a wrong depth, it is three entry points
	// disagreeing about it: whichever number the package settles on, a caller's
	// cost must not depend on which builder they picked.
	T.Run("every entry point stops at the same place", func(t *testing.T) {
		t.Parallel()

		fromTestBuilder := BuildFakeForTest[node](t)

		fromBuilder, err := BuildFake[node]()
		must.NoError(t, err)

		fromMustBuilder := MustBuildFake[node]()

		test.EqOp(t, chainLength(fromTestBuilder), chainLength(fromBuilder))
		test.EqOp(t, chainLength(fromTestBuilder), chainLength(&fromMustBuilder))
	})

	T.Run("that place is the documented default", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, int(DefaultRecursionDepth), chainLength(BuildFakeForTest[node](t)))
	})

	T.Run("a cycle through another type is bounded too", func(t *testing.T) {
		t.Parallel()

		l := BuildFakeForTest[left](t)

		must.NotNil(t, l.Right)
		test.Nil(t, l.Right.Left)
	})
}

func TestBuildFakeForTestToDepth(T *testing.T) {
	T.Parallel()

	T.Run("populates one level per unit of depth", func(t *testing.T) {
		t.Parallel()

		for _, depth := range []uint{0, 1, 2, 3} {
			test.EqOp(t, int(depth), chainLength(BuildFakeForTestToDepth[node](t, depth)))
		}
	})
}

func TestBuildFakeToDepth(T *testing.T) {
	T.Parallel()

	T.Run("populates one level per unit of depth", func(t *testing.T) {
		t.Parallel()

		for _, depth := range []uint{0, 1, 2, 3} {
			actual, err := BuildFakeToDepth[node](depth)
			must.NoError(t, err)
			test.EqOp(t, int(depth), chainLength(actual))
		}
	})

	T.Run("with error", func(t *testing.T) {
		t.Parallel()

		actual, err := BuildFakeToDepth[any](1)
		test.Error(t, err)
		test.Nil(t, actual)
	})
}

func TestMustBuildFakeToDepth(T *testing.T) {
	T.Parallel()

	T.Run("populates one level per unit of depth", func(t *testing.T) {
		t.Parallel()

		for _, depth := range []uint{0, 1, 2, 3} {
			actual := MustBuildFakeToDepth[node](depth)
			test.EqOp(t, int(depth), chainLength(&actual))
		}
	})

	T.Run("with error", func(t *testing.T) {
		t.Parallel()

		test.Panic(t, func() {
			MustBuildFakeToDepth[any](1)
		})
	})
}
