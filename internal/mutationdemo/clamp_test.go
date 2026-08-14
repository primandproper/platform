package mutationdemo

import (
	"testing"

	"github.com/shoenig/test"
)

func TestClamp(t *testing.T) {
	t.Parallel()

	// Only the interior of the range is exercised. Nothing pins either edge,
	// which is the whole interesting property of a clamp.
	test.EqOp(t, 5, Clamp(5, 1, 10))
}
