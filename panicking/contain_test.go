package panicking

import (
	"errors"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestContain(T *testing.T) {
	T.Parallel()

	T.Run("passes an ordinary result through", func(t *testing.T) {
		t.Parallel()

		test.NoError(t, Contain(func() error { return nil }))

		sentinel := errors.New("boom")
		test.ErrorIs(t, Contain(func() error { return sentinel }), sentinel)
	})

	T.Run("converts a panic into a PanicError with the stack", func(t *testing.T) {
		t.Parallel()

		err := Contain(func() error { panic("kaboom") })
		must.Error(t, err)

		var pe *PanicError
		must.True(t, errors.As(err, &pe))
		test.EqOp(t, "kaboom", pe.Value)
		test.EqOp(t, "panic: kaboom", pe.Error())
		// The stack must cover the panic site, not Contain's recovery.
		test.StrContains(t, string(pe.Stack), "TestContain")
	})

	T.Run("preserves an error panic value verbatim", func(t *testing.T) {
		t.Parallel()

		sentinel := errors.New("panicked with an error")
		err := Contain(func() error { panic(sentinel) })

		var pe *PanicError
		must.True(t, errors.As(err, &pe))
		cause, ok := pe.Value.(error)
		must.True(t, ok)
		test.ErrorIs(t, cause, sentinel)
	})
}
