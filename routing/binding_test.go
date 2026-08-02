package routing

import (
	"reflect"
	"testing"

	"github.com/shoenig/test"
)

func Test_setScalar_rejectsOverflow(T *testing.T) {
	T.Parallel()

	// Parsing wide and narrowing via SetInt wraps silently: ?count=300 into an
	// int8 bound to 44, and the handler saw a plausible number instead of the
	// 400 the request had earned.
	T.Run("int8", func(t *testing.T) {
		t.Parallel()

		var v int8
		test.Error(t, setScalar(reflect.ValueOf(&v).Elem(), "300"))
		test.EqOp(t, int8(0), v)
	})

	T.Run("uint8", func(t *testing.T) {
		t.Parallel()

		var v uint8
		test.Error(t, setScalar(reflect.ValueOf(&v).Elem(), "300"))
	})

	T.Run("int16", func(t *testing.T) {
		t.Parallel()

		var v int16
		test.Error(t, setScalar(reflect.ValueOf(&v).Elem(), "40000"))
	})

	T.Run("still accepts an in-range value", func(t *testing.T) {
		t.Parallel()

		var v int8
		test.NoError(t, setScalar(reflect.ValueOf(&v).Elem(), "100"))
		test.EqOp(t, int8(100), v)
	})
}
