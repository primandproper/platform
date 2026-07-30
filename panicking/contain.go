package panicking

import (
	"fmt"
	"runtime/debug"
)

// PanicError is the error a contained panic converts into. Value is what was
// passed to panic; Stack is the panicking goroutine's stack, captured at
// recovery — the recovered value alone rarely names the line, and the
// goroutine that would have printed the trace is the one being rescued.
type PanicError struct {
	Value any
	Stack []byte
}

// Error renders the panic value; the stack is deliberately excluded, because
// it belongs on a span or a log field rather than inside an error string that
// gets wrapped, compared, and truncated.
func (e *PanicError) Error() string {
	return fmt.Sprintf("panic: %v", e.Value)
}

// Contain runs fn, converting a panic into a *PanicError instead of letting it
// unwind the caller's goroutine. Callers that need to distinguish a contained
// panic from an ordinary failure — count it, attach the stack somewhere, wrap
// it in their own sentinel — unwrap with errors.As.
//
// recover only works when called directly by a deferred function, which is why
// this is a wrapper around the work rather than something a caller's own defer
// could delegate to.
func Contain(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = &PanicError{Value: recovered, Stack: debug.Stack()}
		}
	}()

	return fn()
}
