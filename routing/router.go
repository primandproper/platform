package routing

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/primandproper/platform-go/v10/encoding"
	httpx "github.com/primandproper/platform-go/v10/errors/http"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	"github.com/primandproper/platform-go/v10/observability/tracing"

	"github.com/swaggest/openapi-go/openapi3"
	"go.opentelemetry.io/otel/trace"
)

const observerName = "router"

type (
	// Middleware is a standard net/http middleware function.
	Middleware func(http.Handler) http.Handler

	// ErrorEncoder renders a handler or binding error as the status and body to
	// send. It is the seam for a service whose error wire format predates this
	// router: returning (409, myFlatError{Message: "..."}) produces exactly that
	// body, encoded by the route's encoder, instead of the platform APIError
	// envelope.
	//
	// It is not asked whether it wants to handle a given error — a Router either
	// has one or does not, so a service cannot end up with two error formats
	// depending on which error was returned. To fall back to the platform
	// envelope for some errors, call DefaultErrorBody and return what it gives.
	//
	// A nil body writes the status and no body at all. A status outside
	// 100..999 is not a valid HTTP status and would panic the ResponseWriter, so
	// it is written as 500.
	ErrorEncoder func(ctx context.Context, err error) (status int, body any)

	// CodedError is an error carrying the platform ErrorCode that determines its
	// status and appears in the platform envelope. The router's own binding
	// failures implement it, so an ErrorEncoder can tell a malformed body
	// (ErrDecodingRequestInput) from a failed validation (ErrValidatingRequestInput)
	// from a handler's own error without matching on unexported types.
	CodedError interface {
		error
		ErrorCode() httpx.ErrorCode
	}

	// Backend is the pluggable HTTP-muxing seam beneath a Router. A concrete
	// router library (chi, gin, ...) implements it; the Router builds every typed
	// route, spec, and lifecycle concern on top of these primitives and never
	// depends on the library directly.
	//
	// Use must be called before Handle (many muxes, chi included, forbid adding
	// global middleware after routes are registered).
	Backend interface {
		// Handle registers handler for method at pattern. pattern uses the
		// "/users/{id}" placeholder syntax (already stripped of any type
		// annotation by the Router).
		Handle(method, pattern string, handler http.Handler)
		// Use installs global middleware, applied to every route.
		Use(middleware ...Middleware)
		// PathValue returns the value of the named path parameter for req, or ""
		// if absent.
		PathValue(req *http.Request, name string) string
		// Handler returns the composed http.Handler for serving.
		Handler() http.Handler
	}
)

// Router is the declarative, OpenAPI-generating router. It is the primary type
// callers use: typed routes are registered with the package-level generic
// functions (Get, Post, ...), which decode and validate input, encode output,
// and accumulate an OpenAPI 3 operation. A Router is backed by a Backend, so the
// underlying mux (chi, gin, ...) is swappable without changing route code.
//
// It is a concrete type, not an interface, because typed registration must be
// generic and Go does not permit generic interface methods. The swappable seam
// is Backend; the Router is the one fixed orchestration layer above it.
type Router struct {
	backend    Backend
	enc        encoding.ServerEncoderDecoder
	errEncoder ErrorEncoder
	o11y       observability.Observer
	reflector  *openapi3.Reflector

	encoders *encoderCache
	errs     *regErrors

	prefix          string
	tags            []string
	envelopeDefault bool
}

// encoderCache lazily builds and memoizes per-content-type encoders (for routes
// using WithContentType), shared across a Router and its Groups.
type encoderCache struct {
	logger         logging.Logger
	tracerProvider tracing.Provider
	byType         map[encoding.ContentType]encoding.ServerEncoderDecoder
	mu             sync.Mutex
}

func (c *encoderCache) get(contentType encoding.ContentType) encoding.ServerEncoderDecoder {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.byType[contentType]; ok {
		return e
	}

	e := encoding.NewServerEncoderDecoder(contentType, encoding.WithLogger(c.logger), encoding.WithTracerProvider(c.tracerProvider))
	c.byType[contentType] = e

	return e
}

// regErrors accumulates non-fatal registration errors shared across a Router and
// its Groups.
type regErrors struct {
	list []error
	mu   sync.Mutex
}

func (e *regErrors) add(err error) {
	if err == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, err)
}

func (e *regErrors) snapshot() []error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]error(nil), e.list...)
}

type (
	// RouterOption configures a Router at construction.
	RouterOption func(*routerConfig)

	routerConfig struct {
		logger         logging.Logger
		tracerProvider tracing.Provider
		errEncoder     ErrorEncoder
		title          string
		version        string
		description    string
		servers        []string
		envelope       bool
	}
)

// WithTitle sets the OpenAPI document title.
func WithTitle(title string) RouterOption {
	return func(c *routerConfig) { c.title = title }
}

// WithVersion sets the OpenAPI document version.
func WithVersion(version string) RouterOption {
	return func(c *routerConfig) { c.version = version }
}

// WithInfoDescription sets the OpenAPI document description.
func WithInfoDescription(description string) RouterOption {
	return func(c *routerConfig) { c.description = description }
}

// WithServer adds a server URL to the OpenAPI document.
func WithServer(url string) RouterOption {
	return func(c *routerConfig) { c.servers = append(c.servers, url) }
}

// WithDefaultEnvelope sets whether responses are wrapped in
// errors/http.APIResponse[Out] by default (per-route override via WithEnvelope).
func WithDefaultEnvelope(enabled bool) RouterOption {
	return func(c *routerConfig) { c.envelope = enabled }
}

// WithErrorEncoder replaces how returned errors are rendered, for services that
// serve an error wire format this router did not define. The encoder decides the
// status and the body; the route's encoder still serializes it, so content-type
// negotiation and the ServerEncoderDecoder seam are unaffected.
//
// Without it, errors are rendered exactly as they always were — the platform
// APIError envelope, with the status from the code map. With it, they are
// rendered by the encoder for every error, binding failures included; those
// implement CodedError, so the encoder can recover the code it would have been
// given.
//
// It is an option on the Router rather than a per-route one because a service's
// error format is a property of its API, not of one endpoint. A nil encoder
// leaves the default in place.
func WithErrorEncoder(encoder ErrorEncoder) RouterOption {
	return func(c *routerConfig) { c.errEncoder = encoder }
}

// WithLogger attaches a logger.
func WithLogger(logger logging.Logger) RouterOption {
	return func(c *routerConfig) { c.logger = logger }
}

// WithTracerProvider attaches a tracer provider, enabling spans on every
// registered route.
func WithTracerProvider(tracerProvider tracing.Provider) RouterOption {
	return func(c *routerConfig) { c.tracerProvider = tracerProvider }
}

// New builds a Router over a Backend. The backend carries all library-specific
// middleware and OpenTelemetry wiring; the encoder decides how request bodies
// are decoded and responses encoded.
func New(
	backend Backend,
	enc encoding.ServerEncoderDecoder,
	opts ...RouterOption,
) *Router {
	cfg := routerConfig{title: "API", version: "0.0.0", envelope: true}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}

	logger := logging.EnsureLogger(cfg.logger)
	tracerProvider := tracing.EnsureTracerProvider(cfg.tracerProvider)

	reflector := openapi3.NewReflector()
	reflector.Spec.Info.WithTitle(cfg.title).WithVersion(cfg.version)
	if cfg.description != "" {
		reflector.Spec.Info.WithDescription(cfg.description)
	}
	for _, s := range cfg.servers {
		reflector.Spec.Servers = append(reflector.Spec.Servers, openapi3.Server{URL: s})
	}

	return &Router{
		backend:    backend,
		enc:        enc,
		errEncoder: cfg.errEncoder,
		o11y:       observability.NewObserver(observerName, logger, tracerProvider),
		reflector:  reflector,
		encoders: &encoderCache{
			logger:         logger,
			tracerProvider: tracerProvider,
			byType:         map[encoding.ContentType]encoding.ServerEncoderDecoder{},
		},
		errs:            &regErrors{},
		envelopeDefault: cfg.envelope,
	}
}

// Handler returns the composed http.Handler for serving, delegating to the backend.
func (r *Router) Handler() http.Handler { return r.backend.Handler() }

// Use installs global middleware on the backend. Call it before registering routes.
func (r *Router) Use(middleware ...Middleware) { r.backend.Use(middleware...) }

// Backend returns the underlying backend, for advanced use.
func (r *Router) Backend() Backend { return r.backend }

// Err returns a joined error of all non-fatal registration failures accumulated
// so far, or nil if there were none. Check it before serving.
func (r *Router) Err() error {
	return errors.Join(r.errs.snapshot()...)
}

// encoderFor returns the encoder for a route: the default, or a content-type
// specific encoder built (and cached) on demand.
func (r *Router) encoderFor(contentType encoding.ContentType) encoding.ServerEncoderDecoder {
	if contentType == "" {
		return r.enc
	}

	return r.encoders.get(contentType)
}

// writeError maps a handler or binding error to an HTTP status and body and
// encodes it, through the Router's ErrorEncoder if it has one and
// DefaultErrorBody if it does not.
//
// The error is acknowledged on the operation either way: a custom encoder
// changes what the client is told, not what the service records.
func (r *Router) writeError(ctx context.Context, res http.ResponseWriter, op observability.Operation, enc encoding.ServerEncoderDecoder, err error) {
	op.Acknowledge(err, "handling request")

	encode := r.errEncoder
	if encode == nil {
		encode = DefaultErrorBody
	}

	status, body := encode(ctx, err)

	// An out-of-range status would panic the ResponseWriter, taking down a
	// request that was already failing in a way that was going to be reported.
	if status < 100 || status > 999 {
		status = http.StatusInternalServerError
	}

	if body == nil {
		res.WriteHeader(status)

		return
	}

	enc.EncodeResponseWithStatus(ctx, res, body, status)
}

// DefaultErrorBody is the rendering a Router uses when given no ErrorEncoder:
// the platform APIError envelope, with the status from the error's code.
// Binding errors carry their own code; anything else is mapped through
// errors/http.
//
// It is exported so an ErrorEncoder can delegate — a service that wants its own
// format for its own errors and the platform's for everything else returns
// DefaultErrorBody's result for the cases it does not recognize.
//
// The message sent for a binding failure is the bindError's own, not its
// Error() string: the wrapped cause is for the operation record, not for the
// client.
func DefaultErrorBody(ctx context.Context, err error) (status int, body any) {
	if be, ok := errors.AsType[*bindError](err); ok {
		return httpx.HTTPStatusForCode(be.code), httpx.NewAPIErrorResponse(be.msg, be.code, detailsFromCtx(ctx))
	}

	code, msg := httpx.ToAPIError(err)

	return httpx.HTTPStatusForCode(code), httpx.NewAPIErrorResponse(msg, code, detailsFromCtx(ctx))
}

// detailsFromCtx builds response details from the active span (trace ID).
func detailsFromCtx(ctx context.Context) httpx.ResponseDetails {
	details := httpx.ResponseDetails{}
	if sc := trace.SpanContextFromContext(ctx); sc.HasTraceID() {
		details.TraceID = sc.TraceID().String()
	}

	return details
}

// defaultSuccessStatus returns the default success status for a method: 201 for
// POST, 200 otherwise.
func defaultSuccessStatus(method string) int {
	if method == http.MethodPost {
		return http.StatusCreated
	}

	return http.StatusOK
}
