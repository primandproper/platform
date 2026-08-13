package routing

import (
	"errors"

	httpx "github.com/primandproper/platform-go/v10/errors/http"
)

// ErrInvalidResponseStatus reports that a Result named a status an
// http.ResponseWriter cannot carry.
//
// It travels the error path rather than being quietly replaced by the
// registered status. A Result carrying 42 is a defect in the handler, and
// answering the client as though the handler had named nothing hides it for as
// long as nobody reads the response codes. Zero is not this — see Result.
var ErrInvalidResponseStatus = errors.New("response status outside the range an HTTP response can carry")

// Result pairs a handler's response value with the status it is answered with,
// for the routes whose status is not fixed at registration.
//
// The status a route registers is right for almost every route: a POST answers
// 201, a delete answers 204. Two shapes it cannot express are an upsert, which
// answers 201 or 200 depending on what it did to a body that looks the same
// either way, and a readiness probe, which reports one body shape with 200 when
// the service is healthy and 503 when it is not:
//
//	routing.Put(r, "/users/{userID:uuid}", func(ctx context.Context, in upsertUser) (routing.Result[user], error) {
//		u, created, err := svc.Upsert(ctx, in)
//		if err != nil {
//			return routing.Result[user]{}, err
//		}
//
//		status := http.StatusOK
//		if created {
//			status = http.StatusCreated
//		}
//
//		return routing.Result[user]{Value: u, Status: status}, nil
//	}, routing.WithAdditionalResponse(http.StatusCreated, new(user), "created"))
//
// It is opt-in per route: a handler returning T is unaffected, and one
// returning Result[T] is answered exactly as the T inside it would have been,
// at the status it names. The envelope, the generated schema, and the encoded
// bytes are the T's — Result is unwrapped before any of them, never encoded.
//
// # Why the status rides the return
//
// Because it is a return value. Reaching the status through the context would
// put it where nothing in the signature says it can be, and make a handler
// tested by direct call silently unable to set it. Here a handler that names a
// status has said so in the value it returns, and a test reads it off the
// Result without a router.
//
// # Zero
//
// A zero Status means "the registered status", so a handler that names one on
// only some paths does not have to restate the default on the others, and the
// zero Result returned beside an error names nothing.
//
// # Documentation
//
// The registered status is still the documented one. A route that answers more
// declares the others with WithAdditionalResponse, as above. Nothing can infer
// them: the status is chosen per response, and the reflected type says only
// that a Result was returned, not what it will carry.
type Result[T any] struct {
	// Value is the response body, encoded exactly as a handler returning T
	// would have had it encoded.
	Value T

	// Status is the HTTP status to answer with, or zero for the route's
	// registered status.
	Status int
}

// anyResult is how the Router reaches into a Result without knowing what it
// wraps.
//
// Every method is one whose implementation needs T in scope — the value boxed,
// the envelope instantiated at the right type, the schema reflected off the
// wrapped type rather than the wrapper. Go has no way to recover T from a
// reflect.Type and instantiate a generic with it, so the knowledge is carried
// out by methods on Result instead, where T is still in hand.
type anyResult interface {
	// responseStatus is the status the handler named, or zero for none.
	responseStatus() int

	// responseValue is the wrapped value, for an unenveloped response.
	responseValue() any

	// responseEnvelope wraps the value in APIResponse[T] — the same type a
	// handler returning T unenveloped through the Router would produce, so an
	// enveloped Result and an enveloped T are byte-identical on the wire.
	// APIResponse[any] would not be: omitempty treats a typed nil inside an
	// interface as present and a nil *T as absent.
	responseEnvelope(details httpx.ResponseDetails) any

	// responseIsEmpty reports whether the wrapped type is Empty, so that
	// Result[Empty] writes a status and no body exactly as Empty does.
	responseIsEmpty() bool

	// responseStructure is the value whose type is reflected into the
	// operation's documented response — the wrapped type's, never Result's.
	responseStructure(envelope bool) any
}

// Result is the only implementation, and its conformance is what the Router's
// unwrapping depends on.
var _ anyResult = Result[Empty]{}

func (r Result[T]) responseStatus() int { return r.Status }

func (r Result[T]) responseValue() any { return r.Value }

func (r Result[T]) responseEnvelope(details httpx.ResponseDetails) any {
	return httpx.APIResponse[T]{Data: r.Value, Details: details}
}

func (r Result[T]) responseIsEmpty() bool { return isEmptyType[T]() }

func (r Result[T]) responseStructure(envelope bool) any { return responseStructure[T](envelope) }
