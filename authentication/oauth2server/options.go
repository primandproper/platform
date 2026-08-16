package oauth2server

import (
	"time"

	"github.com/primandproper/platform-go/v11/clock"
	"github.com/primandproper/platform-go/v11/observability/logging"
	"github.com/primandproper/platform-go/v11/observability/metrics"
	"github.com/primandproper/platform-go/v11/observability/tracing"
)

// The four lifetimes, and why they are these numbers.
const (
	// DefaultAuthorizationCodeTTL is how long an authorization code is
	// redeemable.
	//
	// One minute. A code is redeemed by the client the instant the browser
	// hands it the redirect, so the window is bounded by one HTTP round trip
	// rather than by anything a human does; RFC 6749 §4.1.2 puts the ceiling at
	// ten minutes, and every second under that is a second the code is not
	// sitting in a browser history, a proxy log, and a Referer header.
	DefaultAuthorizationCodeTTL = time.Minute

	// DefaultAccessTokenTTL is how long an access token is usable.
	//
	// Fifteen minutes, which is the number this package most deliberately did
	// not inherit. The examples this is a generalization of use twenty-four
	// hours, and they use it *because* their store is a map: a restart would
	// otherwise be visible to every signed-in user, so the token had to outlive
	// the process. With a durable store and a rotating refresh token behind it,
	// a long-lived bearer token buys nothing and costs the entire window in
	// which a leaked one works.
	DefaultAccessTokenTTL = 15 * time.Minute

	// DefaultRefreshTokenTTL is how long a refresh token is exchangeable.
	//
	// Seven days. It is the credential that carries revocability here — the
	// thing a sign-out actually ends — and it is one-time-use with reuse
	// detection, so a stolen one is good for one exchange before the theft
	// revokes the whole family.
	DefaultRefreshTokenTTL = 7 * 24 * time.Hour

	// DefaultClientRegistrationTTL is how long a dynamically registered client
	// lasts before it has to register again.
	//
	// Ninety days. Registration is open by construction, so this table has an
	// anonymous writer; an expiry is what bounds it without anybody having to
	// decide which rows are garbage. A client still in use re-registers on its
	// next discovery, which under RFC 7591 it already knows how to do.
	//
	// Set it to zero for registrations that never lapse, and then answer the
	// question of what removes them.
	DefaultClientRegistrationTTL = 90 * 24 * time.Hour
)

type (
	// Option configures a Server at construction.
	Option func(*serverOptions)

	serverOptions struct {
		clock           clock.Clock
		logger          logging.Logger
		tracerProvider  tracing.Provider
		metricsProvider metrics.Provider

		loginRenderer      LoginRenderer
		registrationPolicy RegistrationPolicy

		serviceDocumentation string

		scopes    []string
		resources []string

		codeTTL         time.Duration
		accessTTL       time.Duration
		refreshTTL      time.Duration
		registrationTTL time.Duration

		detectRefreshReuse bool
	}
)

// newServerOptions applies opts over the defaults, ignoring nil entries.
func newServerOptions(opts []Option) *serverOptions {
	o := &serverOptions{
		clock:              clock.NewClock(),
		loginRenderer:      DefaultLoginRenderer,
		registrationPolicy: DefaultRegistrationPolicy,
		codeTTL:            DefaultAuthorizationCodeTTL,
		accessTTL:          DefaultAccessTokenTTL,
		refreshTTL:         DefaultRefreshTokenTTL,
		registrationTTL:    DefaultClientRegistrationTTL,
		detectRefreshReuse: true,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}

	return o
}

// WithAuthorizationCodeTTL sets how long an authorization code is redeemable.
// A non-positive value leaves the default in place; see
// DefaultAuthorizationCodeTTL for what to weigh.
func WithAuthorizationCodeTTL(ttl time.Duration) Option {
	return func(o *serverOptions) {
		if ttl > 0 {
			o.codeTTL = ttl
		}
	}
}

// WithAccessTokenTTL sets how long an access token is usable. A non-positive
// value leaves the default in place.
//
// Raising it is the single change here with the most reach. An access token is
// checked against the store on every resource-server request, so revocation is
// immediate — but only for tokens that are still being checked. Lengthening
// this does not lengthen how long a session lasts; the refresh token already
// decides that. It lengthens how long a leaked token works.
func WithAccessTokenTTL(ttl time.Duration) Option {
	return func(o *serverOptions) {
		if ttl > 0 {
			o.accessTTL = ttl
		}
	}
}

// WithRefreshTokenTTL sets how long a refresh token is exchangeable. A
// non-positive value leaves the default in place.
func WithRefreshTokenTTL(ttl time.Duration) Option {
	return func(o *serverOptions) {
		if ttl > 0 {
			o.refreshTTL = ttl
		}
	}
}

// WithClientRegistrationTTL sets how long a dynamically registered client lasts.
//
// Zero means registrations never lapse, which is a real choice and not a
// mistake — but it is one that leaves an unauthenticated endpoint writing rows
// nothing ever removes, so make it deliberately. A negative value is the same
// as zero.
func WithClientRegistrationTTL(ttl time.Duration) Option {
	return func(o *serverOptions) {
		o.registrationTTL = max(0, ttl)
	}
}

// WithRefreshReuseDetection sets whether presenting an already-redeemed refresh
// token revokes its whole family.
//
// On by default, and turning it off is turning rotation into bookkeeping:
// without it the replay is refused and the copy the attacker is using keeps
// working, so a theft produces one failed request and no other trace. The
// switch exists because the failure mode has a cost — a client that loses the
// response to a refresh and retries revokes its own session — and a deployment
// with a client it cannot fix may need to weigh that.
func WithRefreshReuseDetection(detect bool) Option {
	return func(o *serverOptions) { o.detectRefreshReuse = detect }
}

// WithScopes declares the scopes this server issues.
//
// An authorization request for a scope outside this set is refused rather than
// narrowed. Narrowing silently hands back a token that looks like the one that
// was asked for and is not, and the client finds out at the resource server, in
// a different process, as a 403.
//
// Declaring none accepts any scope the client registered for, which is the
// right answer for a deployment whose resource server does its own scope
// mapping and the wrong one for a deployment that thought this was a filter.
func WithScopes(scopes ...string) Option {
	return func(o *serverOptions) { o.scopes = append(o.scopes, scopes...) }
}

// WithResources declares the RFC 8707 resource indicators this server mints
// tokens for. They become an access token's audience.
//
// Declaring none accepts any resource the client asks for and records it as the
// audience — which still binds the token, but binds it to something the client
// chose. Declaring the set is what makes the audience a statement by this
// server rather than an echo.
func WithResources(resources ...string) Option {
	return func(o *serverOptions) { o.resources = append(o.resources, resources...) }
}

// WithServiceDocumentation sets the service_documentation URL in the discovery
// document.
func WithServiceDocumentation(url string) Option {
	return func(o *serverOptions) { o.serviceDocumentation = url }
}

// WithLoginRenderer replaces the login form. A nil renderer leaves the shipped
// one in place; see DefaultLoginRenderer.
func WithLoginRenderer(renderer LoginRenderer) Option {
	return func(o *serverOptions) {
		if renderer != nil {
			o.loginRenderer = renderer
		}
	}
}

// WithRegistrationPolicy replaces what /register accepts. A nil policy leaves
// the shipped one in place; see DefaultRegistrationPolicy for what that
// enforces and why replacing it should mean adding to it.
func WithRegistrationPolicy(policy RegistrationPolicy) Option {
	return func(o *serverOptions) {
		if policy != nil {
			o.registrationPolicy = policy
		}
	}
}

// WithClock swaps the clock every deadline is stamped against, so a test can
// expire a token without waiting for it.
func WithClock(c clock.Clock) Option {
	return func(o *serverOptions) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithLogger attaches a logger. An absent logger logs nowhere.
func WithLogger(logger logging.Logger) Option {
	return func(o *serverOptions) { o.logger = logger }
}

// WithTracerProvider attaches a tracer provider. An absent one traces nowhere.
func WithTracerProvider(tracerProvider tracing.Provider) Option {
	return func(o *serverOptions) { o.tracerProvider = tracerProvider }
}

// WithMetricsProvider attaches a metrics provider. An absent one records
// nothing.
func WithMetricsProvider(metricsProvider metrics.Provider) Option {
	return func(o *serverOptions) { o.metricsProvider = metricsProvider }
}
