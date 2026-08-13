/*
Package routing provides a declarative, type-safe HTTP router that generates an
OpenAPI 3 specification as routes are registered.

The primary type is Router: a concrete router that owns request decoding,
validation, response encoding, error mapping, and OpenAPI accumulation. Routes
are declared with typed handlers of the form func(ctx, In) (Out, error) via the
package-level generic functions (Get, Post, Put, Patch, Delete, Head). Because Go
interface methods cannot be generic, registration is done with these functions
rather than methods on the Router.

A Router is backed by a Backend — the pluggable seam that a concrete mux library
implements. Implementations ship for chi, the net/http.ServeMux standard library
(stdlib), julienschmidt/httprouter, and gin-gonic/gin. The Router builds
everything on top of the Backend's primitives and never depends on the library
directly, so the underlying router is swappable without touching route code:

	backend := chi.NewBackend(cfg, chi.WithLogger(logger), chi.WithTracerProvider(tracerProvider))
	r := routing.New(backend, encoder, routing.WithLogger(logger), routing.WithTitle("My API"))

	routing.Post(r, "/orgs/{orgID:uint64}/users", createUser, routing.WithSummary("Create user"))
	routing.Get(r, "/orgs/{orgID:uint64}", getOrg)

	r.MountOpenAPI("/openapi.json", "/docs")

A returned error becomes the platform APIError envelope, with the status derived
from the error's platform code. A service with an error wire format of its own
replaces that rendering wholesale with WithErrorEncoder, which decides the status
and the body while leaving serialization to the route's encoder:

	routing.WithErrorEncoder(func(ctx context.Context, err error) (int, any) {
		if errors.Is(err, platformerrors.ErrResourceInUse) {
			return http.StatusConflict, legacyError{Error: "resource is in use"}
		}

		return http.StatusInternalServerError, legacyError{Error: err.Error()}
	})

How an error is recorded follows the status it is sent as: a 5xx is logged at
ERROR and marks the span, a 4xx is logged at WARN and does not. The line between
them is between the service failing and a client sending something the route was
never going to accept — and on an unauthenticated route, recording the second as
the first hands every caller a way to write ERROR lines and error-marked spans
into the service's telemetry. A service that draws the line elsewhere passes
WithErrorClassifier; DefaultErrorSeverity is the rule it replaces.

# Response status

The status a route registers is the one it answers with, per WithResponseStatus,
and that is right for almost every route. Where it is not — an upsert answering
201 or 200 over a body that looks the same either way — the handler returns a
Result instead of a bare value:

	routing.Put(r, "/users/{userID:uuid}", func(ctx context.Context, in upsertUser) (routing.Result[user], error) {
		u, created, err := svc.Upsert(ctx, in)
		if err != nil {
			return routing.Result[user]{}, err
		}

		if !created {
			return routing.Result[user]{Value: u}, nil
		}

		return routing.Result[user]{
			Value:  u,
			Status: http.StatusCreated,
			Header: http.Header{"Location": {"/users/" + u.ID.String()}},
		}, nil
	}, routing.WithAdditionalResponse(http.StatusCreated, new(user), "created"))

A Result carries response headers for the same reason it carries the status:
the ones worth setting per response are the ones a chosen status implies —
Location on the 201, Retry-After on a 503. Content-Type, Content-Length,
Transfer-Encoding, and Connection are refused rather than honored, because the
encoder and net/http set those immediately afterwards and a handler's value
would be overwritten or would truncate the body; see ErrReservedResponseHeader.

Opting in changes nothing a client sees: the Result is unwrapped before
encoding, so the envelope, the generated schema, and the bytes on the wire are
the wrapped type's. A zero Status means the registered one and a nil Header sets
nothing, so the wrapper costs nothing on the paths that use neither.

The status rides the return value because it is one. Reaching it through the
context would put it where the signature says nothing can be, and would leave a
handler called directly in a test silently unable to set it.

Returning an error instead is a different statement, and usually a false one: an
unready readiness probe did what it was asked, and an error would be recorded as
a service fault on every poll. What a status says is what the caller should do
next — retry, re-authenticate, give up — which is also the line severity is drawn
on above. Detail about what happened belongs in the body.

# Request bodies

A body is decoded into the input struct's JSON fields. The case that does not fit
is a body that is itself a document — a GeoJSON polygon, a signed blob — on a
route that also binds parameters: there is no field for it to land in next to the
bound ones. A RawBody field receives it unparsed:

	type putGeoJSON struct {
		AreaID   uuid.UUID       `path:"areaID"`
		Document routing.RawBody
	}

	routing.Put(r, "/areas/{areaID:uuid}/geojson", storeGeoJSON,
		routing.WithRequestContentType("application/geo+json"),
		routing.WithMaxRequestBody(4<<20))

Every route's body can be bounded, raw or decoded, by WithMaxRequestBody for one
route or WithDefaultMaxRequestBody for all of them; a request over the bound is
answered 413 without the handler running. A RawBody route with no bound of its
own gets DefaultRawBodyLimit, because nothing else between the socket and the
handler's []byte forms an opinion about how much to read.

Path parameters use an inline typed syntax — "/users/{id:uint64}" — which drives
both runtime binding and the generated parameter schema. Query, header, cookie,
and body values are bound from struct tags on the typed input.

A path parameter may carry reserved characters. "{name}" is a single segment, so
a value containing a slash goes on the wire percent-escaped — "a/b" as "a%2Fb" —
or the URL addresses something else. Every Backend matches on the escaped path,
which keeps the escaped separator inside the segment, and hands the handler the
decoded value, so a route bound to such a value reads the same on every backend:

	// GET /files/reports%2F2026%2Fq1.csv
	routing.Get(r, "/files/{key}", func(ctx context.Context, in getFile) (*file, error) {
		return store.Fetch(ctx, in.Key) // in.Key == "reports/2026/q1.csv"
	})

# Security

The router does not model security, in either direction.

Enforcement is middleware, declared where the route is registered, next to the
handler it guards:

	routing.Get(r, "/recipes/{id:uuid}", readRecipe,
		routing.WithMiddleware(authz.Require(ReadRecipesPermission)))

The generated document carries no security requirement either. A service that
wants one writes it through Spec(), which returns the live *openapi3.Spec:
SetHTTPBearerTokenSecurity, SetAPIKeySecurity, and SetHTTPBasicSecurity declare
the common schemes, and Components.SecuritySchemesEns() reaches the rest.

Both omissions are deliberate, and the second is why there is no route option for
the first. A requirement recorded on an operation and a requirement enforced at
runtime are two different statements, and a registration option can only make
one of them: it annotates the operation and never sees the request. An option
that made that statement while reading like it made the other would document a
route as protected while it served anyone, which is the one documentation bug
that costs more than no documentation at all.
*/
package routing
