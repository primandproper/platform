// Package consumererr holds the send every messagequeue Consumer uses to report
// a handler or broker failure on the caller's error channel.
//
// messagequeue.Consumer.Consume takes a `chan error` the caller supplies and
// says nothing about how it is drained. Both of the obvious sends are wrong.
// A bare `errs <- err` wedges the consume loop forever against a caller that
// stopped reading — which is every caller, once it has decided to shut down. A
// bare select on ctx.Done() unwedges it, but Go picks uniformly among ready
// cases, so a handler that cancels its own context and then returns an error
// makes both cases ready and drops the error half the time.
//
// Send is the two-phase form that is neither, and it lives here because three
// of the four backends had already written it and the fourth had not.
package consumererr

import "context"

// Send delivers err on errs, blocking until the channel accepts it or ctx is
// done. A nil channel is a caller that does not want errors, and returns
// immediately.
//
// The non-blocking attempt comes first on purpose. It is not an optimization:
// it is what makes delivery win the tie against an already-canceled context,
// so an error raised by a handler that just canceled ctx still reaches the
// caller instead of being dropped by a coin flip.
func Send(ctx context.Context, errs chan<- error, err error) {
	if errs == nil {
		return
	}

	select {
	case errs <- err:
		return
	default:
	}

	select {
	case errs <- err:
	case <-ctx.Done():
	}
}
