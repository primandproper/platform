package routing

import (
	"context"
	"net/http"
)

type (
	// ErrorSeverity is how a Router records an error it is sending to a client.
	//
	// The zero value is SeverityError, so a classifier that falls through its own
	// cases over-reports rather than dropping the error silently. Losing a 500
	// from the logs is the failure mode worth designing against; an extra line is
	// not.
	ErrorSeverity uint8

	// ErrorClassifier decides how a returned error is recorded, given the status
	// it resolved to. It runs after the ErrorEncoder, so it sees the status the
	// client is actually being sent — including one a custom encoder chose.
	//
	// It is not asked to render anything and cannot change the response: the
	// error's status and body are already decided by the time it is called.
	ErrorClassifier func(ctx context.Context, err error, status int) ErrorSeverity
)

const (
	// SeverityError records the error as a service fault: an ERROR log line and
	// an error-marked span.
	SeverityError ErrorSeverity = iota
	// SeverityWarn logs the error at WARN and leaves the span unmarked.
	SeverityWarn
	// SeverityInfo logs the error at INFO and leaves the span unmarked.
	SeverityInfo
	// SeverityNone records nothing at all. It is the honest setting for an error
	// that is a normal outcome of a route — a conditional GET answering 304, an
	// idempotent create answering 409 — and the dishonest one for anything a
	// person would want to find later.
	SeverityNone
)

// String returns the severity's name.
func (s ErrorSeverity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarn:
		return "warn"
	case SeverityInfo:
		return "info"
	case SeverityNone:
		return "none"
	default:
		return "unknown"
	}
}

// DefaultErrorSeverity is how a Router records errors when given no
// ErrorClassifier: 5xx as a service fault, 4xx at WARN, anything else at INFO.
//
// The distinction it draws is between the two things a returned error can mean.
// A 500 is the service failing, and belongs in the logs and on the span as such.
// A 400 is a client sending something the route would never have accepted, and
// recording it as a service fault is wrong twice: it is not a fault of the
// service, and on an unauthenticated route it hands every caller a way to write
// ERROR lines and error-marked spans into the service's telemetry by sending
// malformed requests. The information is not discarded — a 4xx is still logged,
// with the error and the status on the line — it is filed as what it is.
func DefaultErrorSeverity(_ context.Context, _ error, status int) ErrorSeverity {
	switch {
	case status >= http.StatusInternalServerError:
		return SeverityError
	case status >= http.StatusBadRequest:
		return SeverityWarn
	default:
		return SeverityInfo
	}
}
