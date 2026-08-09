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

Path parameters use an inline typed syntax — "/users/{id:uint64}" — which drives
both runtime binding and the generated parameter schema. Query, header, cookie,
and body values are bound from struct tags on the typed input.

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
