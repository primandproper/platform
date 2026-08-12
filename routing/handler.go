package routing

import (
	"net/http"
	"reflect"

	httpx "github.com/primandproper/platform-go/v10/errors/http"
)

// buildHTTPHandler wraps a typed Handler in an http.HandlerFunc that runs the
// full request lifecycle: begin a per-operation span, bind and validate the
// input, invoke the handler, and encode the output (or error).
func buildHTTPHandler[In, Out any](r *Router, plan *bindPlan, rc *routeConfig, h Handler[In, Out]) http.HandlerFunc {
	enc := r.encoderFor(rc.contentType)
	noBody := isEmptyType[Out]()
	successStatus := rc.successStatus
	envelope := rc.envelope
	operationID := rc.operationID

	return func(res http.ResponseWriter, req *http.Request) {
		ctx, op := r.o11y.BeginCustom(req.Context(), operationID)
		defer op.End()

		// The cell SetResponseStatus writes into, installed before binding so
		// that ctx carries it everywhere the handler's does.
		chosen := &responseStatus{}
		ctx = withResponseStatus(ctx, chosen)

		var in In
		if err := plan.bind(ctx, r, res, req, reflect.ValueOf(&in).Elem()); err != nil {
			r.writeError(ctx, res, op, enc, err)

			return
		}

		out, err := h(ctx, in)
		if err != nil {
			r.writeError(ctx, res, op, enc, err)

			return
		}

		status := chosen.resolve(successStatus)

		if noBody {
			res.WriteHeader(status)

			return
		}

		if envelope {
			enc.EncodeResponseWithStatus(ctx, res, httpx.APIResponse[Out]{
				Data:    out,
				Details: detailsFromCtx(ctx),
			}, status)

			return
		}

		enc.EncodeResponseWithStatus(ctx, res, out, status)
	}
}

// isEmptyType reports whether T is the Empty sentinel.
func isEmptyType[T any]() bool {
	var t T
	_, ok := any(t).(Empty)

	return ok
}

// responseStructure returns the value whose type is reflected into the operation's
// success response body: nil for Empty (no body), APIResponse[Out] when enveloped,
// else Out.
func responseStructure[Out any](envelope bool) any {
	if isEmptyType[Out]() {
		return nil
	}

	if envelope {
		return new(httpx.APIResponse[Out])
	}

	return new(Out)
}
