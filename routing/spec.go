package routing

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/swaggest/openapi-go/openapi3"
)

// Spec returns the accumulated OpenAPI 3 specification. It reflects every route
// registered so far; call it after registration (and MountOpenAPI) is complete.
//
// It is the live document, not a copy, so it is also where anything the Router
// does not model gets written. Security is the case that comes up: declare the
// schemes with SetHTTPBearerTokenSecurity, SetAPIKeySecurity, or
// SetHTTPBasicSecurity, reach the rest through Components.SecuritySchemesEns(),
// and put the per-operation requirements on with SetupOperation. Enforcement
// remains a separate matter, and remains middleware.
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

// These pin the Stoplight Elements release the docs page loads, together with
// the subresource-integrity digest of each asset at that release.
//
// Unpinned, the page fetched whatever "latest" happened to be that morning: the
// rendered documentation could change, or break, with no deploy on this side and
// nothing in any changelog anyone here reads. Pinning the version alone still
// trusts unpkg.com to serve the same bytes tomorrow that it served today, which
// is what the digests remove: a substituted asset fails to execute rather than
// executing with our origin's privileges.
//
// The version and the digests are one fact and move together. Bumping the
// version without regenerating both digests does not silently load the new
// release — it blocks it, and the docs page renders empty. Regenerate with:
//
//	curl -sL https://unpkg.com/@stoplight/elements@$VERSION/styles.min.css |
//	  openssl dgst -sha384 -binary | openssl base64 -A
//
// and likewise for web-components.min.js.
const (
	stoplightVersion     = "8.4.6"
	stoplightStylesSRI   = "sha384-oYu9Au1JU1Sd5Za5LYSepn+Sofm8uvVdUCxLWbJYesNAS72Y7G/gQ0pjiB6wyf1Z"
	stoplightElementsSRI = "sha384-aVLrUQSddwM9PSF3tnJ7D2Ob6HUFEXaukrJXb5XJWX2b+gQPMNzj479qnLT85/9T"
)

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
    <link rel="stylesheet" href="https://unpkg.com/@stoplight/elements@%[1]s/styles.min.css" integrity=%[3]q crossorigin="anonymous">
    <script src="https://unpkg.com/@stoplight/elements@%[1]s/web-components.min.js" integrity=%[4]q crossorigin="anonymous"></script>
  </head>
  <body style="margin:0">
    <elements-api apiDescriptionUrl=%[2]q router="hash" layout="sidebar"></elements-api>
  </body>
</html>`, stoplightVersion, specURL, stoplightStylesSRI, stoplightElementsSRI)
}
