package fake

import (
	"testing"

	"github.com/go-faker/faker/v4/pkg/options"
	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// holder is the shape the fan-out option exists for: a slice whose length faker picks,
// of a struct that holds another one.
type holder struct {
	Children []child
	Name     string
}

type child struct {
	Names []string
	Name  string
}

// opaque is the shape the interface option exists for. faker will not invent a value
// for an any, and reports that as an error for the whole struct rather than for the
// field, so a type with one of these is unbuildable without the option.
type opaque struct {
	Payload map[string]any
	Name    string
}

func TestBuildFakeForType(T *testing.T) {
	T.Parallel()

	T.Run("applies this package's defaults when the caller adds none", func(t *testing.T) {
		t.Parallel()

		test.EqOp(t, int(DefaultRecursionDepth), chainLength(BuildFakeForType[node]()))
	})

	T.Run("the caller's option wins over the same one here", func(t *testing.T) {
		t.Parallel()

		// The default bound is 0, so a chain of 3 is the caller's number rather than
		// this package's — which is the whole contract: options are applied over, not
		// alongside.
		test.EqOp(t, 3, chainLength(BuildFakeForType[node](options.WithRecursionMaxDepth(3))))
	})

	T.Run("bounds fan-out", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeForType[holder](options.WithRandomMapAndSliceMaxSize(1))

		must.NotNil(t, actual)
		test.LessEq(t, 1, len(actual.Children))
		for _, c := range actual.Children {
			test.LessEq(t, 1, len(c.Names))
		}
	})

	T.Run("builds a type faker would otherwise refuse", func(t *testing.T) {
		t.Parallel()

		// Without the option this is an error, which is what the plain builders return.
		_, err := BuildFake[opaque]()
		test.Error(t, err)

		actual := BuildFakeForType[opaque](options.WithIgnoreInterface(true))

		must.NotNil(t, actual)
		test.NotEq(t, "", actual.Name)
	})

	T.Run("panics on a type no option can save", func(t *testing.T) {
		t.Parallel()

		test.Panic(t, func() {
			BuildFakeForType[any]()
		})
	})
}
