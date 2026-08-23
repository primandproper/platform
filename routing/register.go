package routing

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/swaggest/openapi-go"
	"github.com/swaggest/openapi-go/openapi3"
)

// Get registers a typed GET handler.
func Get[In, Out any](r *Router, pattern string, h Handler[In, Out], opts ...Option) *Route {
	return register(r, http.MethodGet, pattern, h, opts...)
}

// Post registers a typed POST handler.
func Post[In, Out any](r *Router, pattern string, h Handler[In, Out], opts ...Option) *Route {
	return register(r, http.MethodPost, pattern, h, opts...)
}

// Put registers a typed PUT handler.
func Put[In, Out any](r *Router, pattern string, h Handler[In, Out], opts ...Option) *Route {
	return register(r, http.MethodPut, pattern, h, opts...)
}

// Patch registers a typed PATCH handler.
func Patch[In, Out any](r *Router, pattern string, h Handler[In, Out], opts ...Option) *Route {
	return register(r, http.MethodPatch, pattern, h, opts...)
}

// Delete registers a typed DELETE handler.
func Delete[In, Out any](r *Router, pattern string, h Handler[In, Out], opts ...Option) *Route {
	return register(r, http.MethodDelete, pattern, h, opts...)
}

// Head registers a typed HEAD handler.
func Head[In, Out any](r *Router, pattern string, h Handler[In, Out], opts ...Option) *Route {
	return register(r, http.MethodHead, pattern, h, opts...)
}

// register is the shared implementation behind every verb. It parses the typed
// path, builds and validates the binding plan, records the OpenAPI operation, and
// registers the adapted handler on the backend.
func register[In, Out any](r *Router, method, pattern string, h Handler[In, Out], opts ...Option) *Route {
	plain, pathParams := parsePath(r.prefix + pattern)

	plan := newBindPlan[In](pathParams, method)

	rc := newRouteConfig(method, r)
	for _, o := range opts {
		if o != nil {
			o(rc)
		}
	}
	if rc.operationID == "" {
		rc.operationID = defaultOperationID(method, plain)
	}

	plan.maxBody = resolveMaxRequestBody(rc, plan.rawBody != nil)

	r.recordOperation(method, plain, rc, new(In), responseStructure[Out](rc.envelope), plan.rawBody != nil)

	handler := applyMiddleware(buildHTTPHandler(r, plan, rc, h), rc.middleware)
	r.backend.Handle(method, plain, handler)

	return &Route{Method: method, Path: plain, OperationID: rc.operationID}
}

// resolveMaxRequestBody settles the route's request-body bound: what it or its
// Router asked for, or DefaultRawBodyLimit for a raw body nobody bounded. Zero
// is no bound.
func resolveMaxRequestBody(rc *routeConfig, raw bool) int64 {
	if !rc.maxRequestBodySet {
		if raw {
			return DefaultRawBodyLimit
		}

		return 0
	}

	if rc.maxRequestBody < 0 {
		return 0
	}

	return rc.maxRequestBody
}

// LimitRequestBody bounds a request body at n bytes, as middleware, for a route
// that reads its own body: a Handle route, or a handler standing outside a Router
// altogether. A typed route needs none of it — its bound is WithMaxRequestBody,
// enforced in the binding step.
//
// A request that declares itself over the bound is refused before next runs. One
// that declares nothing — chunked, or lying about its Content-Length — is cut off
// at the bound mid-read, and the handler fails on the read it was going to do
// anyway. Either way the answer is 413 rather than 400: told 400, a client sends
// the same document again.
//
// The refusal is written as plain text, because a Handle route's responses are
// its own and this middleware has no encoder to render an envelope with.
//
// A value of zero or less is no bound, and every request passes through
// untouched.
func LimitRequestBody(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		if n <= 0 {
			return next
		}

		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			if req.ContentLength > n {
				http.Error(res, tooLargeMessage(n), http.StatusRequestEntityTooLarge)

				return
			}

			if req.Body != nil {
				// res is what lets MaxBytesReader stop a client that keeps
				// sending after the bound is reached, rather than reading a body
				// this request has already refused.
				req.Body = http.MaxBytesReader(res, req.Body, n)
			}

			next.ServeHTTP(res, req)
		})
	}
}

// applyMiddleware wraps handler with the given middleware, outermost first.
func applyMiddleware(handler http.Handler, middleware []Middleware) http.Handler {
	for _, m := range slices.Backward(middleware) {
		if m != nil {
			handler = m(handler)
		}
	}

	return handler
}

// recordOperation feeds one registration into the OpenAPI reflector.
func (r *Router) recordOperation(method, plain string, rc *routeConfig, reqStructure, respStructure any, rawBody bool) {
	oc, err := r.reflector.NewOperationContext(method, plain)
	if err != nil {
		r.errs.add(fmt.Errorf("building operation %s %s: %w", method, plain, err))

		return
	}

	oc.SetID(rc.operationID)
	if rc.summary != "" {
		oc.SetSummary(rc.summary)
	}
	if rc.description != "" {
		oc.SetDescription(rc.description)
	}
	if len(rc.tags) > 0 {
		oc.SetTags(rc.tags...)
	}
	if rc.deprecated {
		oc.SetIsDeprecated(true)
	}

	addRequestStructure(oc, rc, reqStructure, rawBody)

	if respStructure != nil {
		oc.AddRespStructure(respStructure, openapi.WithHTTPStatus(rc.successStatus))
	}
	for i := range rc.additionalResponses {
		ar := &rc.additionalResponses[i]
		oc.AddRespStructure(ar.body,
			openapi.WithHTTPStatus(ar.status),
			func(cu *openapi.ContentUnit) { cu.Description = ar.description },
		)
	}

	if addErr := r.reflector.AddOperation(oc); addErr != nil {
		r.errs.add(fmt.Errorf("adding operation %s %s: %w", method, plain, addErr))
	}
}

const (
	// rawContentType is the media type an unparsed body is documented under when
	// the route does not name one: bytes nobody has declared anything about.
	rawContentType = "application/octet-stream"
	// jsonContentType is the media type the reflector files a decoded body
	// under, and so the key WithRequestContentType renames.
	jsonContentType = "application/json"
)

// addRequestStructure records the operation's request. For a route whose body is
// an unparsed document that is two content units rather than one: the input type
// contributes the parameters, and the body is described separately, because the
// reflector cannot derive from a []byte field what the bytes are.
//
// The RawBody field contributes nothing to the first unit — the reflector skips
// fields with no JSON name, which is exactly the shape newBindPlan requires one
// to have — so the operation ends up with one request body rather than two.
func addRequestStructure(oc openapi.OperationContext, rc *routeConfig, reqStructure any, rawBody bool) {
	if !rawBody {
		if rc.requestContentType == "" || rc.requestContentType == jsonContentType {
			oc.AddReqStructure(reqStructure)

			return
		}

		// Renamed after the fact rather than declared up front: a request unit
		// carrying a media type of its own is reflected as an opaque body and
		// nothing else, so declaring one would document the media type and lose
		// every parameter on the route.
		oc.AddReqStructure(reqStructure, renameRequestContentType(rc.requestContentType))

		return
	}

	oc.AddReqStructure(reqStructure)

	contentType := rc.requestContentType
	if contentType == "" {
		contentType = rawContentType
	}

	oc.AddReqStructure(new(RawBody), openapi.WithContentType(contentType), rawBodySchema(contentType))
}

// renameRequestContentType files a reflected request body under the media type
// the route declares instead of the one the reflector assumes. The schema and
// the operation's parameters are whatever reflection made of them; only the key
// changes.
func renameRequestContentType(to string) openapi.ContentOption {
	return openapi.WithCustomize(func(cor openapi.ContentOrReference) {
		body, ok := cor.(*openapi3.RequestBodyOrRef)
		if !ok || body.RequestBody == nil {
			return
		}

		media, ok := body.RequestBody.Content[jsonContentType]
		if !ok {
			return
		}

		delete(body.RequestBody.Content, jsonContentType)
		body.RequestBody.Content[to] = media
	})
}

// rawBodySchema replaces what the reflector makes of a []byte with what the body
// actually is.
//
// Left alone it renders as `{"type": "string"}`, which is wrong in the way that
// matters: a generated client reading that for a GeoJSON route sends a JSON
// string containing a document instead of the document. A JSON media type is
// documented as the empty schema — any JSON, which is what "the router does not
// parse this" means — and anything else as a binary string, which is what
// OpenAPI 3.0 spells an opaque payload.
func rawBodySchema(contentType string) openapi.ContentOption {
	return openapi.WithCustomize(func(cor openapi.ContentOrReference) {
		body, ok := cor.(*openapi3.RequestBodyOrRef)
		if !ok || body.RequestBody == nil {
			return
		}

		media, ok := body.RequestBody.Content[contentType]
		if !ok {
			return
		}

		schema := openapi3.Schema{}
		if !strings.Contains(contentType, "json") {
			schema.WithType(openapi3.SchemaTypeString).WithFormat("binary")
		}

		media.Schema = &openapi3.SchemaOrRef{Schema: &schema}
		body.RequestBody.Content[contentType] = media
	})
}

// Handle registers a raw http.Handler on the backend — an escape hatch for routes
// that do not fit the typed model (static files, streaming, websockets). It
// records no OpenAPI operation.
//
// The Router's default request-body bound reaches these routes too. It has to be
// applied here, because a Handle route does its own reading with no binding step
// to enforce a bound in — and these are the routes most likely to be public and
// handed a large body, so the alternative is that WithDefaultMaxRequestBody
// bounds everything except what most needs bounding.
//
// It goes on outside the route's own middleware, so middleware that reads the
// body — a webhook signature verifier — reads a bounded one. A route that must
// not inherit the bound, or wants a different one, registers through
// Router.MaxRequestBody.
func (r *Router) Handle(method, pattern string, handler http.Handler, middleware ...Middleware) {
	plain, _ := parsePath(r.prefix + pattern)

	handler = applyMiddleware(handler, middleware)
	if r.maxRequestBody > 0 {
		handler = LimitRequestBody(r.maxRequestBody)(handler)
	}

	r.backend.Handle(method, plain, handler)
}

// MaxRequestBody returns a Router that bounds the request bodies of routes
// registered through it at n bytes, whatever WithDefaultMaxRequestBody gave this
// one. Zero or less is no bound.
//
// It is how a Handle route opts out — WithMaxRequestBody(0) is how a typed route
// does, and Handle takes no options, its one variadic being the middleware it
// threads:
//
//	r.MaxRequestBody(0).Handle(http.MethodPost, "/uploads/stream", stream)
//
// Everything else is shared with r, exactly as a Group shares it: the backend,
// the prefix, the tags, the reflector, the error accumulator. A typed route
// registered through it reads the new number as its default too.
func (r *Router) MaxRequestBody(n int64) *Router {
	sub := *r
	sub.maxRequestBody = n

	return &sub
}

// Group creates a sub-Router that shares the backend, reflector, and error
// accumulator, but applies an additional path prefix and default tags to routes
// registered through it.
func (r *Router) Group(prefix string, fn func(sub *Router), tags ...string) {
	sub := *r
	sub.prefix = r.prefix + prefix
	sub.tags = append(append([]string(nil), r.tags...), tags...)
	fn(&sub)
}

// defaultOperationID derives a stable operation ID from the method and path, e.g.
// GET /orgs/{orgID}/users -> "get_orgs_orgID_users".
func defaultOperationID(method, plain string) string {
	replacer := strings.NewReplacer("/", "_", "{", "", "}", "", ":", "_")
	id := strings.Trim(replacer.Replace(plain), "_")
	if id == "" {
		id = "root"
	}

	return strings.ToLower(method) + "_" + id
}
