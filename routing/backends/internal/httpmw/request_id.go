package httpmw

import (
	"net/http"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RequestID returns the request ID assigned to a request by the request-ID
// middleware in Standard (and by chi's identical Use of it), or "" if none is
// present.
//
// Every backend in this module installs chimiddleware.RequestID — chi because
// it is a chi router, the others because the shared stack does — so the value
// is read from chi's context key regardless of which backend served the
// request. Each backend re-exports this under its own name, because a caller
// wiring logging.Logger.SetRequestIDFunc reaches for it beside the backend they
// chose rather than in an internal package they cannot import.
func RequestID(req *http.Request) string {
	if x, ok := req.Context().Value(chimiddleware.RequestIDKey).(string); ok {
		return x
	}

	return ""
}
