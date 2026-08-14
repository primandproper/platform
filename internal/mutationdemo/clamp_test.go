package mutationdemo

import (
	"testing"

	"github.com/shoenig/test"
)

func TestClamp(t *testing.T) {
	t.Parallel()

	// Both edges are inclusive, which is the only interesting property of a
	// clamp and the one the gate reported nothing was asserting.
	test.EqOp(t, 1, Clamp(1, 1, 10))
	test.EqOp(t, 10, Clamp(10, 1, 10))

	test.EqOp(t, 5, Clamp(5, 1, 10))

	test.EqOp(t, 1, Clamp(0, 1, 10))
	test.EqOp(t, 10, Clamp(11, 1, 10))
}
