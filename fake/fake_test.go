package fake

import (
	"testing"

	"github.com/shoenig/test"
)

func TestBuildFkaeTime(T *testing.T) {
	T.Parallel()

	T.Run("simple", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeTime()

		test.False(t, actual.IsZero())
	})
}

type example struct {
	Name string
	Age  int
}

func TestBuildFakeForTest(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		actual := BuildFakeForTest[*example](t)
		test.NotNil(t, actual)
	})
}

func TestMustBuildFake(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotPanic(t, func() {
			actual := MustBuildFake[example]()
			test.NotEq(t, "", actual.Name)
			test.NotEq(t, 0, actual.Age)
		})
	})

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.Panic(t, func() {
			MustBuildFake[any]()
		})
	})
}

func TestBuildFake(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		actual, err := BuildFake[string]()
		test.NoError(t, err)
		test.NotNil(t, actual)
	})

	T.Run("with error", func(t *testing.T) {
		t.Parallel()

		actual, err := BuildFake[any]()
		test.Error(t, err)
		test.Nil(t, actual)
	})
}
