// Package httprouter provides a routing.Backend built on
// julienschmidt/httprouter, a fast radix-tree router. httprouter uses ":name"
// path parameters, so the "/users/{id}" patterns the routing layer produces are
// rewritten to "/users/:id" at registration; path values are read back from the
// request context httprouter populates. The shared observability, recovery,
// CORS, and OpenTelemetry middleware stack is applied around the router,
// matching the chi backend's behavior.
package httprouter

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/metrics"
	"github.com/primandproper/platform-go/v10/observability/tracing"
	"github.com/primandproper/platform-go/v10/routing"
	"github.com/primandproper/platform-go/v10/routing/backends/internal/httpmw"

	hr "github.com/julienschmidt/httprouter"
)

var _ routing.Backend = (*backend)(nil)

// backend is a julienschmidt/httprouter implementation of routing.Backend.
// Global middleware is composed around the router lazily in Handler; because the
// router is wrapped by reference, routes registered after the first Handler call
// are still served.
type backend struct {
	built    http.Handler
	router   *hr.Router
	standard []func(http.Handler) http.Handler
	user     []func(http.Handler) http.Handler
	once     sync.Once
	sealed   atomic.Bool
}

// NewBackend constructs an httprouter-backed routing.Backend with the standard
// middleware and OpenTelemetry stack installed. Pass it to routing.New. Panics
// in handlers propagate to the shared recovery middleware, so no httprouter
// PanicHandler is installed.
func NewBackend(cfg *Config, opts ...Option) routing.Backend {
	// A nil config is the zero config, not a panic. The config subpackage
	// dispatches on Provider and hands whichever sub-config happens to be set —
	// which is nil unless the deployment filled that provider's section in, so
	// every backend here got one on a perfectly ordinary configuration.
	if cfg == nil {
		cfg = &Config{}
	}

	o := newOptions(opts)
	tracerProvider := tracing.EnsureTracerProvider(o.tracerProvider)
	o11y := observability.NewObserver("router", logging.EnsureLogger(o.logger), tracerProvider)

	return &backend{
		router: hr.New(),
		standard: httpmw.Standard(o11y, &httpmw.StackConfig{
			TracerProvider:         tracerProvider,
			MeterProvider:          metrics.EnsureMetricsProvider(o.metricsProvider).MeterProvider(),
			ServiceName:            cfg.ServiceName,
			ValidDomains:           cfg.ValidDomains,
			EnableCORSForLocalhost: cfg.EnableCORSForLocalhost,
			SilenceRouteLogging:    cfg.SilenceRouteLogging,
		}),
	}
}

// Use installs global middleware, applied to every route. It may be called at
// any time before Handler.
// Use appends middleware to the chain.
//
// It must be called before Handler(). The chain is composed once, on the first
// Handler() call, so middleware added afterwards was silently dropped — the
// server ran without the authentication or rate limiting the caller believed it
// had registered. That is now a panic: a middleware that does not run is not a
// condition a process should serve traffic in.
func (b *backend) Use(middleware ...routing.Middleware) {
	if b.sealed.Load() {
		panic("routing: Use called after Handler; middleware must be registered before the handler is built")
	}

	b.user = append(b.user, httpmw.Convert(middleware...)...)
}

// Handle registers handler for method at pattern, rewriting the "{name}"
// placeholders to httprouter's ":name" form.
func (b *backend) Handle(method, pattern string, handler http.Handler) {
	b.router.Handler(method, httpmw.ColonParams(pattern), handler)
}

// PathValue returns the named path parameter from the httprouter params stored
// on the request context.
func (b *backend) PathValue(req *http.Request, name string) string {
	return hr.ParamsFromContext(req.Context()).ByName(name)
}

// Handler returns the composed http.Handler: the standard middleware stack and
// any user middleware wrapped around the router.
func (b *backend) Handler() http.Handler {
	b.sealed.Store(true)

	b.once.Do(func() {
		all := make([]func(http.Handler) http.Handler, 0, len(b.standard)+len(b.user))
		all = append(all, b.standard...)
		all = append(all, b.user...)
		b.built = httpmw.Chain(b.router, all...)
	})

	return b.built
}
