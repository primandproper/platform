package routing_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/primandproper/platform-go/v10/encoding"
	"github.com/primandproper/platform-go/v10/routing"
	"github.com/primandproper/platform-go/v10/routing/backends/chi"
)

// The input for creating a user. Tags decide where each field is bound:
//   - path:  taken from the URL, cross-checked against the {orgID:uint64} token
//   - query: taken from the query string
//   - json (no location tag): part of the request body
type newUserForm struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	OrgID  uint64 `path:"orgID"`
	Notify bool   `query:"notify"`
}

// The typed output. It is encoded into the response (enveloped by default).
type person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	ID    uint64 `json:"id"`
}

// A typed handler: func(ctx, In) (Out, error). The framework decodes and
// validates In, calls this, then encodes Out — or maps a returned error to an
// HTTP status and error envelope.
func createPerson(_ context.Context, in newUserForm) (person, error) {
	return person{ID: in.OrgID*1000 + 1, Name: in.Name, Email: in.Email}, nil
}

func fetchPerson(_ context.Context, in struct {
	OrgID uint64 `path:"orgID"`
	ID    uint64 `path:"userID"`
}) (person, error) {
	return person{ID: in.ID, Name: "Ada"}, nil
}

// Example demonstrates wiring a Router over the chi backend, registering typed
// routes, and mounting the generated OpenAPI spec.
func Example() {
	// The backend is the swappable seam: chi today, gin/etc. tomorrow. It carries
	// the library-specific middleware + OpenTelemetry stack.
	backend := chi.NewBackend(&chi.Config{
		ServiceName: "example-service",
	})

	// The Router is the declarative, OpenAPI-generating layer on top of it.
	enc := encoding.NewServerEncoderDecoder(encoding.ContentTypeJSON)
	r := routing.New(backend, enc,
		routing.WithTitle("Users API"),
		routing.WithVersion("1.0.0"),
	)

	// Typed registration is done with the package-level generic functions.
	// Path params use an inline typed syntax: {orgID:uint64}.
	routing.Post(r, "/orgs/{orgID:uint64}/users", createPerson,
		routing.WithSummary("Create a user"),
		routing.WithTags("users"),
	)

	// Group applies a shared path prefix and default tags.
	r.Group("/orgs/{orgID:uint64}", func(sub *routing.Router) {
		routing.Get(sub, "/users/{userID:uint64}", fetchPerson, routing.WithSummary("Fetch a user"))
	}, "users")

	// Serve the generated OpenAPI 3 spec (and a docs UI) on the same router.
	r.MountOpenAPI("/openapi.json", "/docs")

	// Registration errors (if any) surface here; check before serving.
	if err := r.Err(); err != nil {
		panic(err)
	}

	// In a real service you would hand r.Handler() to an http.Server (or the
	// platform's server/http package). Here we drive one request in-process.
	req := httptest.NewRequest(http.MethodPost, "/orgs/7/users?notify=true",
		strings.NewReader(`{"name":"Ada","email":"ada@example.com"}`))
	req.Header.Set(encoding.ContentTypeHeaderKey, "application/json")

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	fmt.Println("status:", rec.Code)
	fmt.Println("body:", strings.TrimSpace(rec.Body.String()))

	// Output:
	// status: 201
	// body: {"data":{"name":"Ada","email":"ada@example.com","id":7001},"details":{"currentAccountID":"","traceID":""}}
}

// probeReport is one body shape reported with two statuses.
type probeReport struct {
	Detail string `json:"detail"`
	Ready  bool   `json:"ready"`
}

// ExampleSetResponseStatus demonstrates a handler naming the status of one
// response: a readiness probe reports the same body whether or not the service
// is ready, and the status is part of the report.
func ExampleSetResponseStatus() {
	r := routing.New(chi.NewBackend(&chi.Config{ServiceName: "example-service"}),
		encoding.NewServerEncoderDecoder(encoding.ContentTypeJSON))

	ready := false

	routing.Get(r, "/ready", func(ctx context.Context, _ routing.Empty) (probeReport, error) {
		report := probeReport{Ready: ready, Detail: "cache is cold"}

		// Not a returned error: the handler succeeded at reporting, and an error
		// would be recorded as a service fault on every poll.
		if !report.Ready {
			routing.SetResponseStatus(ctx, http.StatusServiceUnavailable)
		}

		return report, nil
	},
		routing.WithEnvelope(false),
		// The registered status is the documented one; the other is declared.
		routing.WithAdditionalResponse(http.StatusServiceUnavailable, new(probeReport), "not ready"),
	)

	if err := r.Err(); err != nil {
		panic(err)
	}

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ready", http.NoBody))

	fmt.Println("status:", rec.Code)
	fmt.Println("body:", strings.TrimSpace(rec.Body.String()))

	// Output:
	// status: 503
	// body: {"detail":"cache is cold","ready":false}
}

// storeArea takes a bound path parameter next to a body the router does not
// parse.
func storeArea(_ context.Context, in struct {
	Document routing.RawBody
	AreaID   uint64 `path:"areaID"`
}) (routing.Empty, error) {
	fmt.Println("area:", in.AreaID)
	fmt.Println("document:", string(in.Document))

	return routing.Empty{}, nil
}

// ExampleRawBody demonstrates a route whose body is a document rather than an
// object with fields, bounded to a size the route chooses.
func ExampleRawBody() {
	r := routing.New(chi.NewBackend(&chi.Config{ServiceName: "example-service"}),
		encoding.NewServerEncoderDecoder(encoding.ContentTypeJSON))

	routing.Put(r, "/areas/{areaID:uint64}/geojson", storeArea,
		routing.WithRequestContentType("application/geo+json"),
		routing.WithMaxRequestBody(4<<20),
		routing.WithResponseStatus(http.StatusNoContent),
	)

	if err := r.Err(); err != nil {
		panic(err)
	}

	req := httptest.NewRequest(http.MethodPut, "/areas/12/geojson",
		strings.NewReader(`{"type":"Point","coordinates":[0,0]}`))
	req.Header.Set(encoding.ContentTypeHeaderKey, "application/geo+json")

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, req)

	fmt.Println("status:", rec.Code)

	// Output:
	// area: 12
	// document: {"type":"Point","coordinates":[0,0]}
	// status: 204
}
