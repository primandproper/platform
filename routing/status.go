package routing

import (
	"context"
)

// responseStatusKey is the context key the per-response status cell travels
// under. It is an empty struct type rather than a string so nothing outside this
// package can collide with it or reach the cell.
type responseStatusKey struct{}

// responseStatus is the cell a handler writes its chosen status into. The Router
// installs one per request and reads it after the handler returns, so the
// handler's write and the router's read are ordered by the return itself.
type responseStatus struct {
	status int
}

// SetResponseStatus names the HTTP status of the response the handler is about
// to return, overriding the one the route was registered with.
//
// The status a route registers is fixed at registration, captured into the
// handler closure, and right for almost every route: a POST answers 201, a
// delete answers 204. The case it cannot express is a successful report of a bad
// state — a readiness probe with one body shape, 200 when the service is healthy
// and 503 when it is not:
//
//	routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (report, error) {
//		rep := check(ctx)
//		if !rep.Ready {
//			routing.SetResponseStatus(ctx, http.StatusServiceUnavailable)
//		}
//
//		return rep, nil
//	}, routing.WithAdditionalResponse(http.StatusServiceUnavailable, new(report), "not ready"))
//
// Returning an error instead would be the wrong statement twice: the handler
// succeeded at what it was asked to do, and a returned error is recorded as one
// — at ERROR, on every poll of a probe that is doing its job.
//
// It applies only to the success path. A handler that sets a status and then
// returns an error is answered with the error's status, because the response the
// client gets is the error's.
//
// The registered status is still the documented one. A route that answers more
// than one declares the others with WithAdditionalResponse, as above; nothing
// here can infer them, since the whole point is that they are chosen per
// response.
//
// It reports whether the status was recorded. False means one of two things: the
// status is outside the 100..999 range an HTTP response can carry, or ctx did
// not come from a routed request — a handler called directly, which is worth
// knowing in a test that expected to be exercising this.
func SetResponseStatus(ctx context.Context, status int) bool {
	if status < minHTTPStatus || status > maxHTTPStatus {
		return false
	}

	cell, ok := ctx.Value(responseStatusKey{}).(*responseStatus)
	if !ok {
		return false
	}

	cell.status = status

	return true
}

// withResponseStatus installs the cell SetResponseStatus writes to.
func withResponseStatus(ctx context.Context, cell *responseStatus) context.Context {
	return context.WithValue(ctx, responseStatusKey{}, cell)
}

// resolve returns the status to send: whatever the handler named, or the
// registered default when it named nothing.
func (s *responseStatus) resolve(registered int) int {
	if s.status != 0 {
		return s.status
	}

	return registered
}
