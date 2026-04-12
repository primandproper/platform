package panicking

import (
	"testing"

	"github.com/shoenig/test"
)

func TestNewProductionPanicker(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		test.NotNil(t, NewProductionPanicker())
	})
}

func Test_stdLibPanicker_Panic(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		p := NewProductionPanicker()

		defer func() {
			test.NotNil(t, recover(), test.Sprint("expected panic to occur"))
		}()

		p.Panic("blah")
	})
}

func Test_stdLibPanicker_Panicf(T *testing.T) {
	T.Parallel()

	T.Run("standard", func(t *testing.T) {
		t.Parallel()

		p := NewProductionPanicker()

		defer func() {
			test.NotNil(t, recover(), test.Sprint("expected panic to occur"))
		}()

		p.Panicf("blah")
	})
}
