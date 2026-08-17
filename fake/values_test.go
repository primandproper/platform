package fake

import (
	"math"
	"testing"

	"github.com/shoenig/test"
)

func TestBuildFakeID(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotEq(t, "", BuildFakeID())
		test.NotEqOp(t, BuildFakeID(), BuildFakeID())
	})
}

func TestBuildFakeNumber(T *testing.T) {
	T.Parallel()

	T.Run("is whole and never zero", func(t *testing.T) {
		t.Parallel()

		for range 20 {
			actual := BuildFakeNumber()

			test.NotEq(t, float64(0), actual)
			test.EqOp(t, math.Trunc(actual), actual)
		}
	})
}

func TestBuildFakeString(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotEq(t, "", BuildFakeString())
		test.NotEqOp(t, BuildFakeString(), BuildFakeString())
	})
}

func TestBuildFakePassword(T *testing.T) {
	T.Parallel()

	T.Run("long enough for the rule a password is usually checked against", func(t *testing.T) {
		t.Parallel()

		test.GreaterEq(t, 8, len(BuildFakePassword()))
	})
}
