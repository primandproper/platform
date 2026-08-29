package oauth2server

import (
	"context"
	stderrors "errors"
	"net/http"
	"slices"
	"strings"

	"github.com/primandproper/platform-go/v13/clock"
	platformerrors "github.com/primandproper/platform-go/v13/errors"
	"github.com/primandproper/platform-go/v13/observability"
	"github.com/primandproper/platform-go/v13/observability/logging"
	"github.com/primandproper/platform-go/v13/observability/metrics"
	"github.com/primandproper/platform-go/v13/observability/tracing"
	"github.com/primandproper/platform-go/v13/routing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// bearerScheme is the RFC 6750 §2.1 authentication scheme, matched
// case-insensitively because RFC 9110 §11.1 says the scheme is.
const bearerScheme = "bearer"

var _ TokenAuthenticator = (*Server)(nil)

// TokenAuthenticator resolves a bearer credential to the access token record
// behind it, and is the seam a Verifier stands on.
//
// *Server implements it, and in the ordinary deployment — resource server and
// authorization server in one process, which is what opaque tokens require —
// that is what a Verifier is handed. It is an interface rather than a *Server
// so that a resource server holding only the Store, or a test wanting a
// controlled answer, has something to pass; it is deliberately the narrowest
// possible one, because a Verifier needs to look a token up and nothing else.
type TokenAuthenticator interface {
	Authenticate(ctx context.Context, bearer string) (*AccessToken, error)
}

// Verifier is the resource server's half of this package: the checks a request
// carrying a bearer token has to survive before a handler sees it.
//
// The authorization server mints tokens; a Verifier decides whether the one in
// front of it authorizes this request, at this resource. Three checks, and the
// package would be incomplete without all three:
//
//   - the token is live. Delegated to TokenAuthenticator, which reads it from
//     the Store — the lookup that opaque tokens are for, and the reason a
//     revoked session stops working now rather than in fifteen minutes.
//   - the token names this resource. RFC 8707 puts the resource identifier in
//     the token's audience so a token minted for one resource server cannot be
//     replayed at another; a resource server that does not compare it has the
//     field and not the property.
//   - the token carries the scopes the route requires.
//
// The second is the one that gets left out, and it is the one this type exists
// for. Server.Authenticate hands back a live token whoever it was minted for,
// because it is the authorization server's lookup and the authorization server
// serves every resource behind it. A deployment running two resource servers
// against one authorization server — an HTTP API and an MCP endpoint, which is
// the shape that keeps arriving — has a cross-resource replay the moment one of
// them treats "Authenticate returned a token" as "this request is authorized".
//
// It is not specific to any protocol built on top of it. An MCP server is an
// http.Handler and mounts behind Middleware; so does a REST API, and a resource
// server with its own response envelope calls Verify and writes its own.
type Verifier struct {
	tokens   TokenAuthenticator
	metadata *ResourceMetadata
	clock    clock.Clock
	o11y     observability.Observer

	ops *metrics.OperationSet

	audienceRejections metrics.Int64Counter

	// resource is the identifier a token's audience is compared against, read
	// once from the metadata document so the comparison and the document a
	// client discovers cannot say different things.
	resource string
}

type (
	// VerifierOption configures a Verifier at construction.
	//
	// A distinct type from Option, with distinct names, because this package
	// already spends WithLogger and its siblings on Server. The alternative —
	// one Option type covering both — would make NewVerifier accept
	// WithLoginRenderer and silently do nothing with it. See ResourceOption,
	// which is prefixed for the same reason.
	VerifierOption func(*verifierOptions)

	verifierOptions struct {
		clock           clock.Clock
		logger          logging.Logger
		tracerProvider  tracing.Provider
		metricsProvider metrics.Provider
	}
)

// newVerifierOptions applies opts over the defaults, ignoring nil entries.
func newVerifierOptions(opts []VerifierOption) *verifierOptions {
	o := &verifierOptions{clock: clock.NewClock()}

	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithVerifierClock replaces the clock a Verifier times its operations against.
func WithVerifierClock(c clock.Clock) VerifierOption {
	return func(o *verifierOptions) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithVerifierLogger attaches a logger. Absent means noop.
func WithVerifierLogger(logger logging.Logger) VerifierOption {
	return func(o *verifierOptions) { o.logger = logger }
}

// WithVerifierTracerProvider attaches a tracer provider. Absent means noop.
func WithVerifierTracerProvider(tracerProvider tracing.Provider) VerifierOption {
	return func(o *verifierOptions) { o.tracerProvider = tracerProvider }
}

// WithVerifierMetricsProvider attaches a metrics provider. Absent means noop.
func WithVerifierMetricsProvider(metricsProvider metrics.Provider) VerifierOption {
	return func(o *verifierOptions) { o.metricsProvider = metricsProvider }
}

// NewVerifier builds the guard a protected resource puts in front of its
// handlers.
//
// Both parameters are parameters rather than options, and neither has a
// default. The metadata is where the resource identifier lives — the string
// every audience check compares against and the document a client follows a 401
// to — so a Verifier without one could publish a resource and authorize
// requests against a different name. The TokenAuthenticator is the lookup; an
// implicit one does not exist, since a resource server that cannot reach the
// store has nothing to verify against.
//
// The resource identifier here and the one in the authorization server's
// WithResources have to be the same string, byte for byte. They are two
// deployment-supplied spellings of one identifier and this package compares
// them rather than deriving one from the other, because the authorization
// server and the resource server are usually not the same process.
func NewVerifier(metadata *ResourceMetadata, tokens TokenAuthenticator, opts ...VerifierOption) (*Verifier, error) {
	if metadata == nil {
		return nil, ErrNilResourceMetadata
	}

	if tokens == nil {
		return nil, ErrNilTokenAuthenticator
	}

	o := newVerifierOptions(opts)

	v := &Verifier{
		tokens:   tokens,
		metadata: metadata,
		clock:    o.clock,
		o11y:     observability.NewObserver(serviceName, o.logger, o.tracerProvider),
		resource: metadata.Document().Resource,
	}

	var err error
	if v.ops, err = metrics.NewOperationSet(o.metricsProvider, serviceName); err != nil {
		return nil, err
	}

	mp := metrics.EnsureMetricsProvider(o.metricsProvider)

	if v.audienceRejections, err = mp.NewInt64Counter(serviceName + "_audience_rejections"); err != nil {
		return nil, platformerrors.Wrap(err, "creating audience rejections counter")
	}

	return v, nil
}

// Metadata returns the document this Verifier guards a resource for, so a
// deployment that built the Verifier has the mountable document without
// carrying both values around.
func (v *Verifier) Metadata() *ResourceMetadata { return v.metadata }

// Verify checks a bearer credential against this resource and returns the token
// behind it.
//
// requiredScopes is the scope set the caller must hold; naming none checks that
// the token is live and minted for this resource and stops there, which is what
// a resource server whose per-operation permissions are decided further in
// wants. That is the opposite convention from authorization/http's Require,
// which denies on an empty permission list — there an empty list is a
// configuration that lost its contents, here it is the ordinary way to say
// "any token for this resource".
//
// The failures are separate sentinels rather than one error, because they are
// four different answers on the wire: ErrNoBearerToken is a request that never
// presented one, ErrNotFound (which ErrExpired wraps) is a token that is not
// usable, ErrTokenAudienceMismatch is a token for somewhere else, and
// ErrInsufficientScope is a good token that is not allowed to do this. Only the
// last is a 403 — the other three are answered by re-presenting a better
// credential, and that one is not.
//
// A token carrying no audience at all fails, and there is no option to accept
// one. See audienceFor: a token minted with no resource indicator is exactly
// the token RFC 8707 exists to prevent, and a resource server that accepts it
// has opted every one of its siblings into being replayed against. A deployment
// seeing this refuse everything has clients that are not sending the resource
// parameter, and that is the thing to fix.
func (v *Verifier) Verify(ctx context.Context, bearer string, requiredScopes ...string) (*AccessToken, error) {
	ctx, op := v.o11y.BeginCustom(ctx, operationName(endpointVerify))
	defer op.End()
	defer op.Time(ctx, v.clock, v.ops.Latency,
		metric.WithAttributes(attribute.String(endpointKey, endpointVerify)))()

	v.ops.Attempt(ctx, metric.WithAttributes(attribute.String(endpointKey, endpointVerify)))
	op.SpanOnly(resourceKey, v.resource)

	if strings.TrimSpace(bearer) == "" {
		return nil, v.refuse(ctx, op, ErrorCodeInvalidRequest, ErrNoBearerToken)
	}

	token, err := v.tokens.Authenticate(ctx, bearer)
	if err != nil {
		if stderrors.Is(err, ErrNotFound) {
			// The ordinary case: every expired session produces one. Counted,
			// not logged — see Server.Authenticate, which declines to record it
			// as a fault for the same reason.
			return nil, v.refuse(ctx, op, ErrorCodeInvalidToken, err)
		}

		return nil, op.Error(err, "verifying bearer token")
	}

	op.Set(clientIDKey, token.ClientID).Set(scopeKey, joinScopes(token.Scopes))

	if !slices.Contains(token.Audience, v.resource) {
		// Its own counter, beside the shared error counter. An unknown token is
		// background — sessions end all day — while a live token minted for
		// another resource arriving here is either a client pointed at the
		// wrong server or a replay, and it is never nothing.
		v.audienceRejections.Add(ctx, 1)

		return nil, v.refuse(ctx, op, ErrorCodeInvalidToken, ErrTokenAudienceMismatch)
	}

	if !token.HasScopes(requiredScopes...) {
		return nil, v.refuse(ctx, op, ErrorCodeInsufficientScope, &scopeError{required: slices.Clone(requiredScopes)})
	}

	return token, nil
}

// refuse counts a refusal and hands the error back, so the checks above are one
// line each.
//
// The refusal is not logged. A resource server that logged every expired token
// would emit a line per stale client per retry, and what is worth alerting on
// is the counter beside it; the request's own span carries which check refused
// it. A broken store is the case that does get logged, and it goes through
// op.Error rather than here.
func (v *Verifier) refuse(ctx context.Context, op observability.Operation, errorCode string, err error) error {
	v.ops.Failed(ctx, metric.WithAttributes(
		attribute.String(endpointKey, endpointVerify),
		attribute.String(errorCodeKey, errorCode),
	))

	op.SpanOnly(errorCodeKey, errorCode)

	return err
}

// scopeError is the refusal a live token gets for lacking a scope, carrying the
// scopes the route required so the challenge can name them.
//
// A type rather than a wrapped sentinel because the challenge needs the list
// back, and a list recovered by parsing an error message is a list that breaks
// the next time the message is reworded. Callers match ErrInsufficientScope,
// which it unwraps to.
type scopeError struct {
	required []string
}

func (e *scopeError) Error() string {
	return ErrInsufficientScope.Error() + ": requires " + joinScopes(e.required)
}

func (e *scopeError) Unwrap() error { return ErrInsufficientScope }

// Middleware admits only requests carrying a live token minted for this
// resource and holding every scope in requiredScopes.
//
// It is a routing.Middleware, which is net/http's own middleware shape, so it
// wraps anything: a routing.Router registration, an http.ServeMux, or a handler
// some other library built. That last one is the case worth naming, because an
// MCP server is an http.Handler and this is the whole of what mounting one
// behind this package's tokens takes.
//
// A handler underneath reads the token with TokenFromContext, which is how a
// per-operation scope check — one MCP tool that writes where the others read —
// gets at what this already looked up, rather than verifying a second time.
func (v *Verifier) Middleware(requiredScopes ...string) routing.Middleware {
	required := slices.Clone(requiredScopes)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
			token, err := v.Verify(req.Context(), BearerFromRequest(req), required...)
			if err != nil {
				v.WriteChallenge(res, err)

				return
			}

			next.ServeHTTP(res, req.WithContext(ContextWithToken(req.Context(), token)))
		})
	}
}

// WriteChallenge writes the refusal a Verify error calls for: the status, and
// the WWW-Authenticate header that tells a client which document to go read.
//
// It is exported because Verify is: a resource server with its own response
// envelope verifies for itself and still wants the challenge header written the
// same way, and the mapping from sentinel to status and RFC 6750 error code is
// exactly the part worth not writing twice.
//
// There is no body. RFC 6750 §3 puts a resource server's refusal in the header,
// and a JSON body beside it would be a second, unspecified copy of the same
// thing for clients to disagree about which to read.
//
// Nothing is logged here. The one failure worth a line is a store that broke,
// and Verify already recorded that on the operation that saw it; every other
// refusal is a client presenting a credential this resource will not take,
// which is the counter's business and not the log's.
func (v *Verifier) WriteChallenge(res http.ResponseWriter, err error) {
	status, challenge := v.challengeFor(err)

	if challenge != "" {
		res.Header().Set("WWW-Authenticate", challenge)
	}

	// The refusal describes a credential, so it is not something to cache.
	res.Header().Set("Cache-Control", "no-store")
	res.WriteHeader(status)
}

// challengeFor maps a Verify error onto the status and challenge it is answered
// with.
func (v *Verifier) challengeFor(err error) (status int, challenge string) {
	switch {
	case stderrors.Is(err, ErrNoBearerToken):
		// No error code. RFC 6750 §3: a request that carried no credential is
		// not one that got it wrong, and naming an error here would have a
		// client reporting a failure where it should be starting discovery.
		return http.StatusUnauthorized, v.metadata.Challenge("", "")

	case stderrors.Is(err, ErrInsufficientScope):
		// The scopes the route wanted go in the challenge, per RFC 6750 §3.1,
		// which is the one refusal here a client can act on: it knows what to
		// ask the authorization server for next time.
		//
		// A bare ErrInsufficientScope carries no list — it is an exported
		// sentinel, so a resource server verifying for itself can hand one back
		// — and that renders a challenge naming no scope rather than failing to
		// render one.
		var required []string

		var scoped *scopeError
		if stderrors.As(err, &scoped) {
			required = scoped.required
		}

		return http.StatusForbidden, v.metadata.ScopeChallenge(
			"the token does not carry the required scope", required)

	case stderrors.Is(err, ErrTokenAudienceMismatch):
		return http.StatusUnauthorized, v.metadata.Challenge(ErrorCodeInvalidToken,
			"the token was not issued for this resource")

	case stderrors.Is(err, ErrNotFound):
		return http.StatusUnauthorized, v.metadata.Challenge(ErrorCodeInvalidToken,
			"the token is expired, revoked, or unknown")

	default:
		// A store that is actually broken. No challenge: the client's
		// credential is not what went wrong, and telling it to re-register
		// would send a retry storm at the authorization server.
		return http.StatusInternalServerError, ""
	}
}

// BearerFromRequest reads the credential out of an Authorization header.
//
// Header only, and that is the same refusal NewResourceMetadata publishes: RFC
// 6750 also defines a form parameter and a query parameter, and the query
// parameter puts a bearer token in every access log and Referer header between
// the client and here. A document that declines to advertise it and an
// extractor that reads it anyway would be advertising it after all.
//
// An absent, malformed, or differently-schemed header yields the empty string,
// which Verify refuses as ErrNoBearerToken.
func BearerFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}

	header := req.Header.Get("Authorization")

	scheme, credential, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) {
		return ""
	}

	return strings.TrimSpace(credential)
}

// tokenContextKey is the key a verified token is carried under. A private
// struct type rather than a string, so nothing outside this package can collide
// with it or overwrite it.
type tokenContextKey struct{}

// ContextWithToken carries a verified access token into a handler's context.
//
// Middleware calls it; it is exported so a test of a handler can build the
// context that handler expects without standing up an authorization server.
func ContextWithToken(ctx context.Context, token *AccessToken) context.Context {
	return context.WithValue(ctx, tokenContextKey{}, token)
}

// TokenFromContext reads the token Middleware verified for this request.
//
// The second return is false for a request that did not come through
// Middleware, which is what a handler mounted somewhere unguarded looks like
// from the inside — so a handler that needs the token must check it rather than
// dereferencing what it hopes is there.
func TokenFromContext(ctx context.Context) (*AccessToken, bool) {
	token, ok := ctx.Value(tokenContextKey{}).(*AccessToken)

	return token, ok
}
