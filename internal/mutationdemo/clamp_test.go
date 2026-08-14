package mutationdemo

import (
	"testing"

	"github.com/shoenig/test"
)

func TestClamp(t *testing.T) {
	t.Parallel()

	// Both edges are asserted, and the gate still reports both boundary
	// mutants as survivors, because both are equivalent.
	test.EqOp(t, 1, Clamp(1, 1, 10))
	test.EqOp(t, 10, Clamp(10, 1, 10))

	test.EqOp(t, 5, Clamp(5, 1, 10))

	test.EqOp(t, 1, Clamp(0, 1, 10))
	test.EqOp(t, 10, Clamp(11, 1, 10))
}

func TestBucket(t *testing.T) {
	t.Parallel()

	// Only either side of the threshold, never the threshold itself.
	test.EqOp(t, "small", Bucket(4, 5))
	test.EqOp(t, "large", Bucket(6, 5))
}
