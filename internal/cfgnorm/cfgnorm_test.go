package cfgnorm

import (
	"testing"

	"github.com/shoenig/test"
)

type sub struct {
	_    struct{}
	Name string
	Size int
}

type withSlice struct {
	Hosts []string
}

func TestZeroToNil(T *testing.T) {
	T.Parallel()

	T.Run("releases a pointer to the zero value", func(t *testing.T) {
		t.Parallel()

		p := &sub{}
		ZeroToNil(&p)
		test.Nil(t, p)
	})

	T.Run("keeps a pointer to a filled value", func(t *testing.T) {
		t.Parallel()

		want := &sub{Name: "configured"}
		got := want
		ZeroToNil(&got)
		test.Eq(t, want, got)
	})

	T.Run("keeps a value filled in only past the first field", func(t *testing.T) {
		t.Parallel()

		want := &sub{Size: 1}
		got := want
		ZeroToNil(&got)
		test.Eq(t, want, got)
	})

	T.Run("leaves an already nil pointer alone", func(t *testing.T) {
		t.Parallel()

		var p *sub
		ZeroToNil(&p)
		test.Nil(t, p)
	})

	T.Run("handles a type a == comparison could not", func(t *testing.T) {
		t.Parallel()

		empty := &withSlice{}
		ZeroToNil(&empty)
		test.Nil(t, empty)

		filled := &withSlice{Hosts: []string{"localhost"}}
		kept := filled
		ZeroToNil(&kept)
		test.Eq(t, filled, kept)
	})
}
