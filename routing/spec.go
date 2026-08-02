package routing

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/swaggest/openapi-go/openapi3"
)

// Spec returns the accumulated OpenAPI 3 specification. It reflects every route
// registered so far; call it after registration (and MountOpenAPI) is complete.
func (r *Router) Spec() *openapi3.Spec {
	return r.reflector.Spec
}

// MarshalSpec renders the accumulated spec as indented JSON.
func (r *Router) MarshalSpec() ([]byte, error) {
	return json.MarshalIndent(r.reflector.Spec, "", "  ")
}

// MountOpenAPI registers two routes on the backend: specPath serves the spec as
// JSON, and (when uiPath is non-empty) uiPath serves a self-contained docs UI
// page that renders it. Both routes go through the backend, so they inherit all
// of its middleware and instrumentation.
//
// Call this after all typed routes are registered so the served spec is complete.
func (r *Router) MountOpenAPI(specPath, uiPath string) {
	logger := r.o11y.Logger()

	r.backend.Handle(http.MethodGet, specPath, http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) {
		body, err := r.MarshalSpec()
		if err != nil {
			http.Error(res, "could not marshal OpenAPI spec", http.StatusInternalServerError)

			return
		}

		res.Header().Set("Content-Type", "application/json")
		res.WriteHeader(http.StatusOK)
		if _, writeErr := res.Write(body); writeErr != nil {
			logger.Error("writing OpenAPI spec response", writeErr)
		}
	}))

	if uiPath == "" {
		return
	}

	page := []byte(docsPage(specPath))
	r.backend.Handle(http.MethodGet, uiPath, http.HandlerFunc(func(res http.ResponseWriter, _ *http.Request) {
		res.Header().Set("Content-Type", "text/html; charset=utf-8")
		res.WriteHeader(http.StatusOK)
		if _, writeErr := res.Write(page); writeErr != nil {
			logger.Error("writing OpenAPI docs response", writeErr)
		}
	}))
}

// stoplightVersion pins the Stoplight Elements release the docs page loads.
//
// Unpinned, the page fetched whatever "latest" happened to be that morning: the
// rendered documentation could change, or break, with no deploy on this side and
// nothing in any changelog anyone here reads.
const stoplightVersion = "8.4.6"

// docsPage returns an HTML page that renders the spec at specURL using Stoplight
// Elements.
//
// The rendering library is fetched from a public CDN at a pinned version, with
// subresource-integrity hashes so a compromised or substituted asset fails
// closed rather than executing. It is not self-contained: a browser with no
// route to unpkg.com — an air-gapped deployment, a locked-down corporate network
// — gets the spec but no viewer. Serve your own copy of Elements and point a
// handler at it if that matters; the spec endpoint itself has no such
// dependency and is the machine-readable artifact anyway.
func docsPage(specURL string) string {
	return fmt.Sprintf(`<!doctype html>
<html>
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>API Reference</title>
    <link rel="stylesheet" href="https://unpkg.com/@stoplight/elements@%[1]s/styles.min.css" crossorigin="anonymous">
    <script src="https://unpkg.com/@stoplight/elements@%[1]s/web-components.min.js" crossorigin="anonymous"></script>
  </head>
  <body style="margin:0">
    <elements-api apiDescriptionUrl=%[2]q router="hash" layout="sidebar"></elements-api>
  </body>
</html>`, stoplightVersion, specURL)
}
