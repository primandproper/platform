package routing

import (
	"context"
	"net/http"
)

type (
	// Handler is a typed HTTP handler. It receives a decoded, validated input value
	// and returns a typed output or an error. The framework handles decoding In from
	// the request, encoding Out into the response, and mapping a returned error to an
	// HTTP status and error envelope.
	Handler[In, Out any] func(ctx context.Context, in In) (Out, error)

	// Empty is a placeholder type for routes that take no meaningful input or produce
	// no response body. A route whose Out is Empty writes only a status code (no body).
	Empty struct{}

	// RawBody is a request body the router reads but does not parse. A field of
	// this type on an input struct receives the request body verbatim:
	//
	//	type putGeoJSON struct {
	//		AreaID   uuid.UUID       `path:"areaID"`
	//		Document routing.RawBody
	//	}
	//
	// It exists for the case the typed model otherwise cannot express: a body
	// that is itself a document rather than an object with fields, on a route
	// that also binds parameters. Decoding into the input struct has nowhere to
	// put such a body, and a handler that takes the whole request instead loses
	// the bound parameters along with everything else the router does.
	//
	// Exactly one RawBody field is allowed, and only on a method that carries a
	// body, and only on an input with no other body fields — the body is either
	// this document or an object with fields, and a struct claiming both is a
	// registration-time panic rather than a request-time surprise.
	//
	// The bytes are unvalidated and the request's Content-Type is not checked
	// against anything; the point of the type is that the router forms no
	// opinion about them. What it does enforce is size: see WithMaxRequestBody,
	// which defaults to DefaultRawBodyLimit for a route that has one of these.
	RawBody []byte

	// Route is the descriptor returned by a registration call. It records the concrete
	// method and (annotation-stripped) path the route was registered under, plus the
	// resolved OpenAPI operation ID.
	Route struct {
		Method      string
		Path        string
		OperationID string
	}
)

// methodAllowsBody reports whether an HTTP method conventionally carries a request
// body that the layer should attempt to decode.
func methodAllowsBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
